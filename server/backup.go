// backup.go — S3-compatible backup, central server push, and scheduled operations.
package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	_ "modernc.org/sqlite" // driver for VACUUM INTO
)

// Per-operation timeout for S3 and HTTP push operations.
// Prevents a single unreachable target from blocking the entire backup loop.
const pushTimeout = 2 * time.Minute

type BackupStatus struct {
	LastConfigPush time.Time `json:"last_config_push,omitempty"`
	LastFullBackup time.Time `json:"last_full_backup,omitempty"`
	LastError      string    `json:"last_error,omitempty"`
	LastErrorTime  time.Time `json:"last_error_time,omitempty"`
	NextScheduled  time.Time `json:"next_scheduled,omitempty"`
	CurrentSlot    int       `json:"current_slot"`
}

type ringMeta struct {
	CurrentSlot int        `json:"currentSlot"`
	Slots       []slotInfo `json:"slots"`
}

type slotInfo struct {
	Slot      int       `json:"slot"`
	Timestamp time.Time `json:"timestamp"`
	SizeBytes int64     `json:"sizeBytes"`
}

type manifestStation struct {
	Name       string `json:"name"`
	Prefix     string `json:"prefix"`
	Hostname   string `json:"hostname"`
	LastBackup string `json:"last_backup"`
}

type manifestFile struct {
	Stations map[string]manifestStation `json:"stations"`
}

type BackupManager struct {
	store    *Store
	dbPath   string
	dataDir  string
	mu       sync.Mutex
	status   BackupStatus
	configCh chan struct{}
	fullCh   chan struct{}
}

// NewBackupManager creates a backup manager for S3 and central server backups.
func NewBackupManager(store *Store, dbPath string) *BackupManager {
	return &BackupManager{
		store:    store,
		dbPath:   dbPath,
		dataDir:  filepath.Dir(dbPath),
		configCh: make(chan struct{}, 1),
		fullCh:   make(chan struct{}, 1),
	}
}

// NotifyConfigChange signals that config.json changed. Non-blocking.
func (bm *BackupManager) NotifyConfigChange() {
	select {
	case bm.configCh <- struct{}{}:
	default:
	}
}

// TriggerFull signals a manual full backup. Non-blocking.
func (bm *BackupManager) TriggerFull() {
	select {
	case bm.fullCh <- struct{}{}:
	default:
	}
}

// Status returns a snapshot of the current backup status.
func (bm *BackupManager) Status() BackupStatus {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	return bm.status
}

// Run is the main backup loop. It listens for config-change signals,
// manual triggers, and a periodic timer.
func (bm *BackupManager) Run(ctx context.Context) {
	// Initial delay to let the server finish starting
	select {
	case <-ctx.Done():
		return
	case <-time.After(5 * time.Second):
	}

	for {
		settings := bm.store.GetSettings()
		periodMin := settings.Backup.PeriodicMinutes
		if periodMin <= 0 {
			periodMin = 24 * 60 // effectively disabled
		}
		timer := time.NewTimer(time.Duration(periodMin) * time.Minute)

		bm.mu.Lock()
		bm.status.NextScheduled = time.Now().Add(time.Duration(periodMin) * time.Minute)
		bm.mu.Unlock()

		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-bm.configCh:
			timer.Stop()
			bm.pushConfig(ctx)
		case <-bm.fullCh:
			timer.Stop()
			bm.pushFull(ctx)
		case <-timer.C:
			bm.pushFull(ctx)
		}
	}
}

// pushConfig reads config.json and pushes it to all enabled targets.
func (bm *BackupManager) pushConfig(ctx context.Context) {
	settings := bm.store.GetSettings()
	bs := settings.Backup
	if !bs.S3Enabled && !bs.CentralEnabled {
		return
	}

	data, err := os.ReadFile(bm.store.Path())
	if err != nil {
		bm.recordError(fmt.Errorf("read config: %w", err))
		return
	}

	ts := time.Now().UTC().Format("20060102T150405Z")
	anyOK := false

	if bs.S3Enabled {
		if err := bm.pushConfigS3(ctx, bs, settings, ts, data); err != nil {
			bm.recordError(fmt.Errorf("s3 config push: %w", err))
		} else {
			anyOK = true
		}
	}
	if bs.CentralEnabled {
		if err := bm.pushConfigCentral(ctx, bs, settings.StationID, data); err != nil {
			bm.recordError(fmt.Errorf("central config push: %w", err))
		} else {
			anyOK = true
		}
	}

	if anyOK {
		bm.mu.Lock()
		bm.status.LastConfigPush = time.Now()
		bm.mu.Unlock()
		log.Println("[backup] config pushed")
	}
}

// pushFull pushes config + a gzipped DB snapshot to all enabled targets.
func (bm *BackupManager) pushFull(ctx context.Context) {
	settings := bm.store.GetSettings()
	bs := settings.Backup
	if !bs.S3Enabled && !bs.CentralEnabled {
		return
	}

	// Push config first (reuses pushConfig logic)
	bm.pushConfig(ctx)

	// Create DB snapshot
	snapPath, err := bm.createDBSnapshot()
	if err != nil {
		bm.recordError(fmt.Errorf("db snapshot: %w", err))
		return
	}
	defer os.Remove(snapPath)

	gzPath, gzSize, err := bm.gzipFile(snapPath)
	if err != nil {
		bm.recordError(fmt.Errorf("gzip: %w", err))
		return
	}
	defer os.Remove(gzPath)

	slot := bm.nextSlot()
	anyOK := false

	if bs.S3Enabled {
		if err := bm.pushDBS3(ctx, bs, settings, slot, gzPath, gzSize); err != nil {
			bm.recordError(fmt.Errorf("s3 db push: %w", err))
		} else {
			anyOK = true
		}
	}
	if bs.CentralEnabled {
		if err := bm.pushDBCentral(ctx, bs, settings.StationID, slot, gzPath); err != nil {
			bm.recordError(fmt.Errorf("central db push: %w", err))
		} else {
			anyOK = true
		}
	}

	if anyOK {
		bm.mu.Lock()
		bm.status.LastFullBackup = time.Now()
		bm.status.CurrentSlot = slot
		bm.mu.Unlock()
		log.Printf("[backup] full backup pushed (slot %d)", slot)
	}
}

// createDBSnapshot creates a consistent copy of the database via VACUUM INTO.
func (bm *BackupManager) createDBSnapshot() (string, error) {
	snapPath := filepath.Join(bm.dataDir, "backup-snap.db")

	// Remove any stale snapshot from a previous crashed run.
	// VACUUM INTO requires the destination to not exist.
	os.Remove(snapPath)

	db, err := sql.Open("sqlite", bm.dbPath)
	if err != nil {
		return "", err
	}
	defer db.Close()

	_, err = db.Exec(fmt.Sprintf(`VACUUM INTO '%s'`, snapPath))
	if err != nil {
		return "", err
	}
	return snapPath, nil
}

// gzipFile compresses src to src.gz, returning the path and size.
// On error, any partial .gz file is cleaned up.
func (bm *BackupManager) gzipFile(src string) (string, int64, error) {
	gzPath := src + ".gz"

	srcFile, err := os.Open(src)
	if err != nil {
		return "", 0, err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(gzPath)
	if err != nil {
		return "", 0, err
	}

	gw := gzip.NewWriter(dstFile)
	_, copyErr := io.Copy(gw, srcFile)
	closeGzErr := gw.Close()
	closeFileErr := dstFile.Close()

	// Check all errors; clean up partial file on any failure
	if err := firstErr(copyErr, closeGzErr, closeFileErr); err != nil {
		os.Remove(gzPath)
		return "", 0, err
	}

	info, err := os.Stat(gzPath)
	if err != nil {
		os.Remove(gzPath)
		return "", 0, err
	}
	return gzPath, info.Size(), nil
}

func (bm *BackupManager) nextSlot() int {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	return (bm.status.CurrentSlot + 1) % 3
}

func stationPrefix(settings Settings) string {
	name := Slugify(settings.StationName)
	if name == "" || name == "screen" {
		name = "station"
	}
	id := settings.StationID
	short := id
	if len(id) >= 8 {
		short = id[len(id)-8:]
	}
	return name + "-" + short
}

func s3Client(bs BackupSettings) (*minio.Client, error) {
	return minio.New(bs.S3Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(bs.S3AccessKey, bs.S3SecretKey, ""),
		Secure: bs.S3UseSSL,
		Region: bs.S3Region,
	})
}

func (bm *BackupManager) pushConfigS3(ctx context.Context, bs BackupSettings, settings Settings, ts string, data []byte) error {
	opCtx, cancel := context.WithTimeout(ctx, pushTimeout)
	defer cancel()

	client, err := s3Client(bs)
	if err != nil {
		return err
	}

	prefix := stationPrefix(settings)

	// PUT latest.json
	latestKey := prefix + "/config/latest.json"
	if _, err := client.PutObject(opCtx, bs.S3Bucket, latestKey,
		bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "application/json"}); err != nil {
		return fmt.Errorf("put latest.json: %w", err)
	}

	// PUT history/{ts}.json
	histKey := prefix + "/config/history/" + ts + ".json"
	if _, err := client.PutObject(opCtx, bs.S3Bucket, histKey,
		bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "application/json"}); err != nil {
		return fmt.Errorf("put history: %w", err)
	}

	// Update manifest (best-effort)
	if err := bm.updateManifest(opCtx, client, bs.S3Bucket, settings); err != nil {
		log.Printf("[backup] warning: failed to update manifest: %v", err)
	}

	return nil
}

func (bm *BackupManager) pushDBS3(ctx context.Context, bs BackupSettings, settings Settings, slot int, gzPath string, size int64) error {
	opCtx, cancel := context.WithTimeout(ctx, pushTimeout)
	defer cancel()

	client, err := s3Client(bs)
	if err != nil {
		return err
	}

	prefix := stationPrefix(settings)

	// PUT snap-{slot}.db.gz
	snapKey := fmt.Sprintf("%s/db/snap-%d.db.gz", prefix, slot)
	f, err := os.Open(gzPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := client.PutObject(opCtx, bs.S3Bucket, snapKey,
		f, size,
		minio.PutObjectOptions{ContentType: "application/gzip"}); err != nil {
		return fmt.Errorf("put snap: %w", err)
	}

	// Update meta.json (ring buffer tracking)
	meta := readS3Meta(opCtx, client, bs.S3Bucket, prefix)
	meta.CurrentSlot = slot
	found := false
	for i := range meta.Slots {
		if meta.Slots[i].Slot == slot {
			meta.Slots[i].Timestamp = time.Now().UTC()
			meta.Slots[i].SizeBytes = size
			found = true
			break
		}
	}
	if !found {
		meta.Slots = append(meta.Slots, slotInfo{
			Slot:      slot,
			Timestamp: time.Now().UTC(),
			SizeBytes: size,
		})
	}

	metaData, _ := json.MarshalIndent(meta, "", "  ")
	metaKey := prefix + "/db/meta.json"
	if _, err := client.PutObject(opCtx, bs.S3Bucket, metaKey,
		bytes.NewReader(metaData), int64(len(metaData)),
		minio.PutObjectOptions{ContentType: "application/json"}); err != nil {
		return fmt.Errorf("put meta.json: %w", err)
	}

	return nil
}

// readS3Meta loads the ring-buffer metadata from S3.
// Returns empty metadata if the key doesn't exist or can't be parsed.
// Note: minio GetObject doesn't error on missing keys — the error
// surfaces on Read, so we handle decode failure gracefully.
func readS3Meta(ctx context.Context, client *minio.Client, bucket, prefix string) ringMeta {
	metaKey := prefix + "/db/meta.json"
	obj, err := client.GetObject(ctx, bucket, metaKey, minio.GetObjectOptions{})
	if err != nil {
		return ringMeta{}
	}
	defer obj.Close()

	var meta ringMeta
	if err := json.NewDecoder(obj).Decode(&meta); err != nil {
		return ringMeta{}
	}
	return meta
}

// updateManifest does a read-modify-write of the bucket-root _manifest.json.
func (bm *BackupManager) updateManifest(ctx context.Context, client *minio.Client, bucket string, settings Settings) error {
	manifest := manifestFile{Stations: make(map[string]manifestStation)}

	// Try to read existing manifest (GetObject is lazy — errors surface on Read)
	obj, err := client.GetObject(ctx, bucket, "_manifest.json", minio.GetObjectOptions{})
	if err == nil {
		json.NewDecoder(obj).Decode(&manifest)
		obj.Close()
	}
	if manifest.Stations == nil {
		manifest.Stations = make(map[string]manifestStation)
	}

	hostname, _ := os.Hostname()
	manifest.Stations[settings.StationID] = manifestStation{
		Name:       settings.StationName,
		Prefix:     stationPrefix(settings),
		Hostname:   hostname,
		LastBackup: time.Now().UTC().Format(time.RFC3339),
	}

	data, _ := json.MarshalIndent(manifest, "", "  ")
	_, err = client.PutObject(ctx, bucket, "_manifest.json",
		bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "application/json"})
	return err
}

func (bm *BackupManager) pushConfigCentral(ctx context.Context, bs BackupSettings, stationID string, data []byte) error {
	opCtx, cancel := context.WithTimeout(ctx, pushTimeout)
	defer cancel()

	url := strings.TrimRight(bs.CentralURL, "/") + "/" + stationID + "/config"
	req, err := http.NewRequestWithContext(opCtx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("central returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (bm *BackupManager) pushDBCentral(ctx context.Context, bs BackupSettings, stationID string, slot int, gzPath string) error {
	opCtx, cancel := context.WithTimeout(ctx, pushTimeout)
	defer cancel()

	url := strings.TrimRight(bs.CentralURL, "/") + "/" + stationID + "/db"
	f, err := os.Open(gzPath)
	if err != nil {
		return err
	}
	defer f.Close()

	req, err := http.NewRequestWithContext(opCtx, http.MethodPost, url, f)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/gzip")
	req.Header.Set("X-Slot", fmt.Sprintf("%d", slot))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("central returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (bm *BackupManager) recordError(err error) {
	log.Printf("[backup] error: %v", err)
	bm.mu.Lock()
	bm.status.LastError = err.Error()
	bm.status.LastErrorTime = time.Now()
	bm.mu.Unlock()
}

// firstErr returns the first non-nil error, or nil.
func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
