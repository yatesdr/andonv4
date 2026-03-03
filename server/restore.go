// restore.go — Database restore from S3 or central server backup.
package server

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
)

// RestoreFromS3 restores config and database from an S3-compatible store.
func RestoreFromS3(dataDir string, bs BackupSettings, stationRef string) error {
	client, err := s3Client(bs)
	if err != nil {
		return fmt.Errorf("create s3 client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	prefix, err := resolveStation(ctx, client, bs.S3Bucket, stationRef)
	if err != nil {
		return fmt.Errorf("resolve station: %w", err)
	}

	// Restore config
	configKey := prefix + "/config/latest.json"
	obj, err := client.GetObject(ctx, bs.S3Bucket, configKey, minio.GetObjectOptions{})
	if err != nil {
		return fmt.Errorf("get config: %w", err)
	}
	configData, err := io.ReadAll(obj)
	obj.Close()
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	configPath := filepath.Join(dataDir, "config.json")
	if err := atomicWrite(configPath, configData); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	fmt.Printf("Restored config to %s\n", configPath)

	// Read meta.json to find newest slot
	metaKey := prefix + "/db/meta.json"
	metaObj, err := client.GetObject(ctx, bs.S3Bucket, metaKey, minio.GetObjectOptions{})
	if err != nil {
		return fmt.Errorf("get meta.json: %w", err)
	}
	var meta ringMeta
	if err := json.NewDecoder(metaObj).Decode(&meta); err != nil {
		metaObj.Close()
		return fmt.Errorf("decode meta.json: %w", err)
	}
	metaObj.Close()

	if len(meta.Slots) == 0 {
		fmt.Println("No database snapshots found in meta.json")
		return nil
	}

	// Find newest slot
	newest := meta.Slots[0]
	for _, s := range meta.Slots[1:] {
		if s.Timestamp.After(newest.Timestamp) {
			newest = s
		}
	}

	snapKey := fmt.Sprintf("%s/db/snap-%d.db.gz", prefix, newest.Slot)
	snapObj, err := client.GetObject(ctx, bs.S3Bucket, snapKey, minio.GetObjectOptions{})
	if err != nil {
		return fmt.Errorf("get snapshot: %w", err)
	}
	defer snapObj.Close()

	dbPath := filepath.Join(dataDir, "andon.db")
	if err := gunzipToFile(snapObj, dbPath); err != nil {
		return fmt.Errorf("restore db: %w", err)
	}

	// Remove stale WAL/SHM files
	os.Remove(dbPath + "-wal")
	os.Remove(dbPath + "-shm")

	fmt.Printf("Restored database to %s (slot %d, %s)\n", dbPath, newest.Slot, newest.Timestamp.Format(time.RFC3339))
	return nil
}

func resolveStation(ctx context.Context, client *minio.Client, bucket, ref string) (string, error) {
	obj, err := client.GetObject(ctx, bucket, "_manifest.json", minio.GetObjectOptions{})
	if err != nil {
		// No manifest — try ref as literal prefix
		return ref, nil
	}
	defer obj.Close()

	var manifest manifestFile
	if err := json.NewDecoder(obj).Decode(&manifest); err != nil {
		return ref, nil
	}

	// Check if ref matches a station UUID
	if station, ok := manifest.Stations[ref]; ok {
		return station.Prefix, nil
	}

	// Check if ref matches a prefix
	for _, station := range manifest.Stations {
		if station.Prefix == ref {
			return station.Prefix, nil
		}
	}

	// Check if ref is a partial UUID suffix
	for uuid, station := range manifest.Stations {
		if strings.HasSuffix(uuid, ref) {
			return station.Prefix, nil
		}
	}

	// Fall back to using ref as literal prefix
	return ref, nil
}

// RestoreFromCentral restores config and database from the central HTTP server.
func RestoreFromCentral(dataDir string, centralURL, stationID string) error {
	client := &http.Client{Timeout: 5 * time.Minute}

	// Restore config
	configURL := strings.TrimRight(centralURL, "/") + "/" + stationID + "/config"
	resp, err := client.Get(configURL)
	if err != nil {
		return fmt.Errorf("get config: %w", err)
	}
	configData, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("central returned %d for config: %s", resp.StatusCode, string(configData))
	}

	configPath := filepath.Join(dataDir, "config.json")
	if err := atomicWrite(configPath, configData); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	fmt.Printf("Restored config to %s\n", configPath)

	// Restore database
	dbURL := strings.TrimRight(centralURL, "/") + "/" + stationID + "/db"
	dbResp, err := client.Get(dbURL)
	if err != nil {
		return fmt.Errorf("get db: %w", err)
	}
	defer dbResp.Body.Close()
	if dbResp.StatusCode >= 300 {
		body, _ := io.ReadAll(dbResp.Body)
		return fmt.Errorf("central returned %d for db: %s", dbResp.StatusCode, string(body))
	}

	dbPath := filepath.Join(dataDir, "andon.db")
	if err := gunzipToFile(dbResp.Body, dbPath); err != nil {
		return fmt.Errorf("restore db: %w", err)
	}

	os.Remove(dbPath + "-wal")
	os.Remove(dbPath + "-shm")

	fmt.Printf("Restored database to %s\n", dbPath)
	return nil
}

func gunzipToFile(r io.Reader, destPath string) error {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gr.Close()

	// Write to temp file, then atomic rename
	tmp := destPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, gr); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, destPath)
}

func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
