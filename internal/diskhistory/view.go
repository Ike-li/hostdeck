package diskhistory

import (
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Ike-li/hostdeck/internal/plugin"
)

func text(en, zh string) plugin.Text {
	return plugin.Text{ByLang: map[string]string{"en": en, "zh-CN": zh}}
}

func size(bytes int64) string {
	if bytes == 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	v, unit := math.Abs(float64(bytes)), 0
	for v >= 1000 && unit < len(units)-1 {
		v /= 1000
		unit++
	}
	digits := 2
	if v >= 10 {
		digits = 1
	}
	if unit == 0 || (v >= 100 && unit < 3) {
		digits = 0
	}
	sign := ""
	if bytes < 0 {
		sign = "−"
	}
	return fmt.Sprintf("%s%.*f %s", sign, digits, v, units[unit])
}

func delta(bytes int64) string {
	if bytes > 0 {
		return "+" + size(bytes)
	}
	return size(bytes)
}

type field struct {
	Name  plugin.Text `json:"name"`
	Value string      `json:"value"`
}

type action struct {
	ID    string      `json:"id"`
	Label plugin.Text `json:"label"`
}

type item struct {
	ID      string      `json:"id"`
	Name    plugin.Text `json:"name"`
	Status  string      `json:"status"`
	Value   string      `json:"value"`
	Fields  []field     `json:"fields,omitempty"`
	Log     string      `json:"log,omitempty"`
	Actions []action    `json:"actions,omitempty"`
}

type row struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func baseline(scans []snapshot) (snapshot, snapshot, bool) {
	latest := scans[len(scans)-1]
	base := scans[0]
	for _, s := range scans {
		if !s.Time.After(latest.Time.Add(-24 * time.Hour)) {
			base = s
		}
	}
	return base, latest, len(scans) > 1 && !base.Time.Equal(latest.Time)
}

func difference(base, latest snapshot, path string) (int64, bool) {
	a, aok := base.Sizes[path]
	b, bok := latest.Sizes[path]
	// Missing summaries mean zero only when their parent was actually read,
	// and no directory summaries were dropped at the storage limit.
	if !aok && (base.Sizes["."].Partial || base.Truncated) {
		return 0, false
	}
	if !bok && (latest.Sizes["."].Partial || latest.Truncated) {
		return 0, false
	}
	if a.Partial || b.Partial {
		return 0, false
	}
	return b.Bytes - a.Bytes, true
}

func changed(base, latest snapshot) []string {
	names := map[string]bool{}
	for name := range base.Sizes {
		if name != "." {
			names[name] = true
		}
	}
	for name := range latest.Sizes {
		if name != "." {
			names[name] = true
		}
	}
	paths := make([]string, 0, len(names))
	for name := range names {
		paths = append(paths, name)
	}
	slices.SortFunc(paths, func(a, b string) int {
		da, _ := difference(base, latest, a)
		db, _ := difference(base, latest, b)
		if math.Abs(float64(da)) > math.Abs(float64(db)) {
			return -1
		}
		if math.Abs(float64(da)) < math.Abs(float64(db)) {
			return 1
		}
		return strings.Compare(a, b)
	})
	return paths
}

func (e *entry) card(targets []string, now time.Time) any {
	status := "ok"
	value := "—"
	label := text("No scans yet. Open to scan or start hourly tracking.", "尚无扫描记录，打开后可扫描或开始每小时跟踪。")
	var rows []row
	for i, target := range targets {
		scans := e.state.Scans[target]
		if len(scans) == 0 {
			status = "warn"
			continue
		}
		base, latest, compared := baseline(scans)
		if latest.Sizes["."].Partial || now.Sub(latest.Time) > 2*interval {
			status = "warn"
		}
		if i != 0 {
			continue
		}
		value = size(latest.Sizes["."].Bytes)
		label = text("Logical file size · scan again to see changes", "文件逻辑大小 · 再次扫描后显示变化")
		if compared {
			if d, ok := difference(base, latest, "."); ok {
				value = delta(d)
				span := latest.Time.Sub(base.Time).Hours()
				label = text(fmt.Sprintf("Change over %.1f hours · %s total", span, size(latest.Sizes["."].Bytes)), fmt.Sprintf("过去 %.1f 小时变化 · 共 %s", span, size(latest.Sizes["."].Bytes)))
			} else {
				label = text("Partial scan · change is unknown", "扫描不完整 · 无法确定增减")
			}
			for _, name := range changed(base, latest) {
				if filepath.Dir(name) != "." {
					continue
				}
				if d, ok := difference(base, latest, name); ok && d != 0 {
					rows = append(rows, row{name, delta(d)})
				}
				if len(rows) == 3 {
					break
				}
			}
		}
		if latest.Sizes["."].Partial {
			label = text("Partial scan · measured size is a lower bound; change is unknown", "扫描不完整 · 大小仅为下限，无法确定总量增减")
		}
		if now.Sub(latest.Time) > 2*interval {
			label.ByLang["en"] += fmt.Sprintf(" · last scan %.0f hours ago", now.Sub(latest.Time).Hours())
			label.ByLang["zh-CN"] += fmt.Sprintf(" · 最后扫描于 %.0f 小时前", now.Sub(latest.Time).Hours())
		}
	}
	if e.running {
		label = text("Scanning · showing the previous saved reading", "正在扫描 · 显示上次保存的读数")
	}
	if e.state.Error != "" {
		status = "warn"
		label = text("Last scan failed: "+e.state.Error, "上次扫描失败："+e.state.Error)
	}
	return struct {
		Status string      `json:"status"`
		Value  string      `json:"value"`
		Label  plugin.Text `json:"label"`
		Rows   []row       `json:"rows,omitempty"`
	}{status, value, label, rows}
}

func history(scans []snapshot, path string) string {
	var lines []string
	start := max(0, len(scans)-60)
	for i := start; i < len(scans); i++ {
		m, ok := scans[i].Sizes[path]
		if !ok {
			continue
		}
		coverage := ""
		if m.Partial {
			coverage = " · partial"
		}
		lines = append(lines, scans[i].Time.UTC().Format("2006-01-02 15:04:05.000 UTC")+" · "+size(m.Bytes)+coverage)
	}
	return strings.Join(lines, "\n")
}

func (e *entry) view(targets []string) any {
	var items []item
	for index, target := range targets {
		it := item{ID: id(target), Name: plugin.Text{All: target}, Status: "warn", Value: "—"}
		it.Fields = []field{{text("Logical file bytes, not allocated disk space", "文件逻辑字节数，不等于磁盘占用"), "B"}}
		if e.state.Tracking {
			it.Fields = append(it.Fields, field{text("Tracking interval while hostdeck runs", "hostdeck 运行时的跟踪间隔"), "1h"})
		} else {
			it.Fields = append(it.Fields, field{text("Hourly tracking is paused", "每小时跟踪已暂停"), "—"})
		}
		if e.running {
			it.Fields = append(it.Fields, field{text("Active scan; refresh for results", "当前扫描目录，请刷新查看结果"), e.target})
			it.Actions = append(it.Actions, action{"cancel", text("Cancel scan", "取消扫描")})
		} else {
			it.Actions = append(it.Actions, action{"scan", text("Scan now", "立即扫描")})
		}
		if index == 0 {
			if e.state.Tracking {
				it.Actions = append(it.Actions, action{"pause", text("Pause hourly tracking", "暂停每小时跟踪")})
			} else if !e.running {
				it.Actions = append(it.Actions, action{"track", text("Start hourly tracking", "开始每小时跟踪")})
			}
		}
		if e.state.Error != "" {
			it.Fields = append(it.Fields, field{text("Last scan failed", "上次扫描失败"), e.state.Error})
		}
		scans := e.state.Scans[target]
		if len(scans) == 0 {
			it.Fields = append(it.Fields, field{text("No scans yet; scanning starts only after confirmation", "尚无记录，确认后才会开始扫描"), "—"})
			items = append(items, it)
			continue
		}
		base, latest, compared := baseline(scans)
		it.Value, it.Status = size(latest.Sizes["."].Bytes), "ok"
		it.Fields = append(it.Fields, field{text("Last scan (UTC)", "上次扫描（UTC）"), latest.Time.UTC().Format(time.RFC3339)}, field{text("Saved snapshots", "已保存快照"), fmt.Sprint(len(scans))})
		if latest.Sizes["."].Partial {
			it.Status = "warn"
			it.Fields = append(it.Fields, field{text("Partial: unreadable entries; measured size is a lower bound", "扫描不完整：有条目无法读取，大小仅为下限"), fmt.Sprint(latest.Skipped)})
		}
		if time.Since(latest.Time) > 2*interval {
			it.Status = "warn"
			it.Fields = append(it.Fields, field{text("Last scan is over two hours old", "最后扫描已超过两小时"), latest.Time.UTC().Format(time.RFC3339)})
		}
		if e.state.Error != "" {
			it.Status = "warn"
		}
		if latest.Truncated {
			it.Fields = append(it.Fields, field{text("Directory summaries capped; root total is retained", "目录摘要已达上限，仍保留根目录总量"), fmt.Sprint(maxDirs)})
		}
		if compared {
			if d, ok := difference(base, latest, "."); ok {
				it.Fields = append(it.Fields, field{text("Change since baseline", "相对基准变化"), delta(d)})
			}
			it.Fields = append(it.Fields, field{text("Baseline (UTC)", "基准时间（UTC）"), base.Time.UTC().Format(time.RFC3339)})
		}
		it.Log = history(scans, ".")
		items = append(items, it)
		paths := changed(base, latest)
		items[len(items)-1].Fields = append(items[len(items)-1].Fields, field{text("Directory summaries shown (up to three levels)", "显示的目录摘要（最多三层）"), fmt.Sprintf("%d / %d", min(50, len(paths)), len(paths))})
		for _, name := range paths[:min(50, len(paths))] {
			m, exists := latest.Sizes[name]
			child := item{ID: id(target) + ":" + name, Name: plugin.Text{All: filepath.Join(target, name)}, Status: "ok", Value: size(m.Bytes), Log: history(scans, name)}
			if m.Partial || (!exists && (latest.Truncated || latest.Sizes["."].Partial)) {
				child.Status = "warn"
				child.Value = "—"
			}
			if compared {
				if d, ok := difference(base, latest, name); ok {
					child.Fields = append(child.Fields, field{text("Change since baseline", "相对基准变化"), delta(d)})
				}
			}
			items = append(items, child)
		}
	}
	return struct {
		Items []item `json:"items"`
	}{items}
}
