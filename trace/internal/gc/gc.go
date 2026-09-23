// Package gc removes expired events and orphan objects (per-project retention when configured).
package gc

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/laststate/trace/internal/metrics"
	"github.com/laststate/trace/internal/objects"
	"github.com/laststate/trace/internal/store"
)

type Runner struct {
	Store         *store.Store
	Object        *objects.Store
	RetentionDays int // global fallback for issue pipeline
	Log           *slog.Logger
}

func (r *Runner) RunOnce(ctx context.Context) error {
	if r.Log == nil {
		r.Log = slog.Default()
	}
	if r.Store != nil {
		projects, err := r.Store.ListProjectsAll(ctx)
		if err != nil {
			// fallback global
			if r.RetentionDays > 0 {
				return r.purgeKeys(ctx, func() ([]string, error) {
					return r.Store.PurgeOldEvents(ctx, r.RetentionDays, 500)
				})
			}
		} else {
			for _, p := range projects {
				ps, err := r.Store.GetOrCreateSettings(ctx, p.ID)
				if err != nil {
					continue
				}
				// issue/crash events
				days := ps.RetentionEventsDays
				if days <= 0 {
					days = r.RetentionDays
				}
				if days > 0 {
					_ = r.purgeKeys(ctx, func() ([]string, error) {
						return r.Store.PurgeByPipeline(ctx, p.ID, "issue", days, 200)
					})
					_ = r.purgeKeys(ctx, func() ([]string, error) {
						return r.Store.PurgeByPipeline(ctx, p.ID, "crash", days, 200)
					})
				}
				if ps.RetentionHealthDays > 0 {
					_ = r.purgeKeys(ctx, func() ([]string, error) {
						return r.Store.PurgeByPipeline(ctx, p.ID, "health", ps.RetentionHealthDays, 200)
					})
				}
				if ps.RetentionLogsDays > 0 {
					_ = r.purgeKeys(ctx, func() ([]string, error) {
						return r.Store.PurgeByPipeline(ctx, p.ID, "log", ps.RetentionLogsDays, 200)
					})
				}
				if ps.RetentionMetricsDays > 0 {
					_ = r.purgeKeys(ctx, func() ([]string, error) {
						return r.Store.PurgeByPipeline(ctx, p.ID, "metric", ps.RetentionMetricsDays, 200)
					})
				}
			}
		}
	}
	return r.sweepOrphans(ctx)
}

func (r *Runner) purgeKeys(ctx context.Context, fn func() ([]string, error)) error {
	keys, err := fn()
	if err != nil {
		return err
	}
	for _, k := range keys {
		if r.Object != nil {
			if err := r.Object.Delete(k); err != nil {
				r.Log.Warn("delete expired object", "key", k, "err", err)
			} else {
				metrics.GCDeleted.Add(1)
			}
		}
	}
	if len(keys) > 0 {
		r.Log.Info("retention", "purged", len(keys))
	}
	return nil
}

func (r *Runner) sweepOrphans(ctx context.Context) error {
	if r.Object == nil || r.Store == nil {
		return nil
	}
	refs, err := r.Store.ReferencedObjectKeys(ctx)
	if err != nil {
		return err
	}
	keys, err := r.Object.ListKeys()
	if err != nil {
		if r.Object.Endpoint != "" {
			r.Log.Warn("s3 list orphans", "err", err)
			return nil
		}
		return r.walkFSOrphans(ctx, refs)
	}
	for _, key := range keys {
		if _, ok := refs[key]; ok {
			continue
		}
		if r.Object.Endpoint == "" {
			p := r.Object.Path(key)
			if st, err := os.Stat(p); err == nil && time.Since(st.ModTime()) < time.Hour {
				continue
			}
		}
		if err := r.Object.Delete(key); err != nil {
			r.Log.Warn("orphan delete", "key", key, "err", err)
			continue
		}
		r.Store.LogOrphanGC(ctx, key, "unreferenced")
		metrics.GCDeleted.Add(1)
	}
	return nil
}

func (r *Runner) walkFSOrphans(ctx context.Context, refs map[string]struct{}) error {
	root := r.Object.Root
	var orphans []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.Contains(path, string(filepath.Separator)+".cache"+string(filepath.Separator)) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		key := filepath.ToSlash(rel)
		parts := strings.Split(key, "/")
		if len(parts) != 3 {
			return nil
		}
		if _, ok := refs[key]; !ok {
			if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) > time.Hour {
				orphans = append(orphans, key)
			}
		}
		return nil
	})
	for _, k := range orphans {
		if err := r.Object.Delete(k); err != nil {
			continue
		}
		r.Store.LogOrphanGC(ctx, k, "unreferenced")
		metrics.GCDeleted.Add(1)
	}
	return nil
}
