// Package sink - 磁盘兜底队列 (Disk Spool).
//
// 目的:
//   当 ClickHouse 彻底不可用 (宕机/网络隔离/磁盘满)时, 避免采集数据丢失.
//   ClickHouseSink 的 sendWithRetry 重试 3 次仍失败的 batch, 调用 Spool.Write()
//   写入本地 NDJSON 文件. Replayer 后台协程定期扫描 spool 目录, 尝试重新 INSERT.
//
// 文件命名:
//   {unix_nano}-{hostname}-{seq}.ndjson
//   按时间戳排序, 保证 FIFO 回放.
//
// 容量控制:
//   目录总大小超 DiskSpoolMaxBytes 时, 新写入的 batch 直接丢弃并 +failed 指标.
package sink

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/thb10086/new-api-logger/gateway/internal/capture"
	"github.com/thb10086/new-api-logger/gateway/internal/config"
)

// Spool 磁盘兜底队列.
type Spool struct {
	cfg    *config.Config
	logger *slog.Logger
	dir    string
	seq    atomic.Uint64

	// 指标
	spooledFiles   atomic.Uint64
	spooledRecords atomic.Uint64
	replayedFiles  atomic.Uint64
	replayedOK     atomic.Uint64
	replayedFail   atomic.Uint64
	droppedFull    atomic.Uint64

	// replayer 控制
	ch     *ClickHouseSink
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewSpool 创建 Spool, 自动建目录.
func NewSpool(cfg *config.Config, logger *slog.Logger) (*Spool, error) {
	if !cfg.DiskSpoolEnabled {
		return nil, nil
	}
	if err := os.MkdirAll(cfg.DiskSpoolDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建 spool 目录失败: %w", err)
	}
	return &Spool{
		cfg:    cfg,
		logger: logger.With("component", "spool"),
		dir:    cfg.DiskSpoolDir,
		stopCh: make(chan struct{}),
	}, nil
}

// AttachClickHouse 绑定 CH sink 用于回放.
func (s *Spool) AttachClickHouse(ch *ClickHouseSink) {
	if s == nil {
		return
	}
	s.ch = ch
}

// Start 启动后台 replayer.
func (s *Spool) Start(ctx context.Context) {
	if s == nil || !s.cfg.DiskSpoolEnabled {
		return
	}
	s.wg.Add(1)
	go s.replayLoop(ctx)
	s.logger.Info("spool 启动",
		"dir", s.dir,
		"interval", s.cfg.DiskSpoolInterval,
		"max_bytes", s.cfg.DiskSpoolMaxBytes,
	)
}

// Shutdown 优雅停止.
func (s *Spool) Shutdown() {
	if s == nil {
		return
	}
	close(s.stopCh)
	s.wg.Wait()
}

// Write 把失败的 batch 写入 spool.
// 目录已满时直接丢弃并 +droppedFull.
func (s *Spool) Write(batch []*capture.Record) error {
	if s == nil || !s.cfg.DiskSpoolEnabled || len(batch) == 0 {
		return nil
	}

	// 容量检查 (非精确, 粗粒度统计目录大小)
	if sz, _ := s.dirSize(); sz >= s.cfg.DiskSpoolMaxBytes {
		s.droppedFull.Add(uint64(len(batch)))
		if s.droppedFull.Load()%1000 == 0 {
			s.logger.Error("spool 已满, 丢弃记录",
				"dir_size_bytes", sz,
				"max_bytes", s.cfg.DiskSpoolMaxBytes,
				"dropped_total", s.droppedFull.Load(),
			)
		}
		return fmt.Errorf("spool full")
	}

	name := fmt.Sprintf("%d-%d.ndjson", time.Now().UnixNano(), s.seq.Add(1))
	tmpPath := filepath.Join(s.dir, name+".tmp")
	finalPath := filepath.Join(s.dir, name)

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("打开 spool 文件: %w", err)
	}

	bw := bufio.NewWriter(f)
	enc := json.NewEncoder(bw)
	n := 0
	for _, rec := range batch {
		if err := enc.Encode(rec); err != nil {
			s.logger.Warn("spool 序列化失败, 跳过", "err", err, "request_id", rec.RequestID)
			continue
		}
		n++
	}
	if err := bw.Flush(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("flush spool: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("fsync spool: %w", err)
	}
	f.Close()

	// 原子 rename, 保证 replayer 不会读到半写文件
	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename spool: %w", err)
	}

	s.spooledFiles.Add(1)
	s.spooledRecords.Add(uint64(n))
	s.logger.Warn("CH 写入失败, 已 spool 到磁盘",
		"file", name, "records", n)
	return nil
}

// SpoolStats 指标快照.
type SpoolStats struct {
	SpooledFiles   uint64
	SpooledRecords uint64
	ReplayedFiles  uint64
	ReplayedOK     uint64
	ReplayedFail   uint64
	DroppedFull    uint64
	DirSizeBytes   int64
	PendingFiles   int
}

func (s *Spool) Stats() SpoolStats {
	if s == nil {
		return SpoolStats{}
	}
	size, pending := s.dirStats()
	return SpoolStats{
		SpooledFiles:   s.spooledFiles.Load(),
		SpooledRecords: s.spooledRecords.Load(),
		ReplayedFiles:  s.replayedFiles.Load(),
		ReplayedOK:     s.replayedOK.Load(),
		ReplayedFail:   s.replayedFail.Load(),
		DroppedFull:    s.droppedFull.Load(),
		DirSizeBytes:   size,
		PendingFiles:   pending,
	}
}

// ============ 内部 ============

func (s *Spool) replayLoop(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(s.cfg.DiskSpoolInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.replayOnce(ctx)
		}
	}
}

func (s *Spool) replayOnce(ctx context.Context) {
	if s.ch == nil {
		return
	}
	files, err := s.listSpoolFiles()
	if err != nil || len(files) == 0 {
		return
	}

	for _, fp := range files {
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		default:
		}

		if err := s.replayFile(ctx, fp); err != nil {
			s.replayedFail.Add(1)
			// 失败不删除, 下一轮重试
			continue
		}
		if err := os.Remove(fp); err != nil {
			s.logger.Warn("删除已回放 spool 文件失败", "file", fp, "err", err)
		}
		s.replayedFiles.Add(1)
	}
}

func (s *Spool) replayFile(ctx context.Context, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}

	// 直接以 NDJSON 形式重投 CH (复用 CH sink 的 HTTP 逻辑)
	if err := s.ch.sendRaw(ctx, bytes.NewReader(data)); err != nil {
		s.logger.Warn("spool 回放失败, 下轮重试", "file", filepath.Base(path), "err", err)
		return err
	}
	// 粗略统计回放记录数 (行数)
	ok := bytes.Count(data, []byte("\n"))
	s.replayedOK.Add(uint64(ok))
	s.logger.Info("spool 回放成功", "file", filepath.Base(path), "records", ok)
	return nil
}

// listSpoolFiles 列出 .ndjson 文件, 按文件名 (时间戳) 升序.
func (s *Spool) listSpoolFiles() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) != ".ndjson" {
			continue
		}
		out = append(out, filepath.Join(s.dir, name))
	}
	sort.Strings(out)
	return out, nil
}

func (s *Spool) dirSize() (int64, error) {
	var total int64
	err := filepath.WalkDir(s.dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err == nil {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

func (s *Spool) dirStats() (int64, int) {
	var total int64
	var pending int
	_ = filepath.WalkDir(s.dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if filepath.Ext(d.Name()) == ".ndjson" {
			pending++
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total, pending
}
