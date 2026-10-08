package main

import (
	"time"

	"github.com/Seykhel/rclonetop/internal/collect"
	"github.com/Seykhel/rclonetop/internal/model"
	"github.com/Seykhel/rclonetop/internal/theme"
	"github.com/Seykhel/rclonetop/internal/ui"
)

const lastStep = 12

func demoOptions() ui.Options {
	// A literal clock label and absent host timestamps keep the film independent
	// of timezone, recording date and the machine that renders it.
	return ui.Options{Theme: theme.Vivid(), Host: "demo-host", ClockLayout: "SIMULATED", UpdateMS: 1000}
}

// scenario builds fresh snapshots for each step: once handed to Bubble Tea,
// their slices are never mutated by the producer. No collector observes the host.
func scenario(step int) []collect.Result {
	const mib = 1 << 20
	at := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	mount := model.Process{PID: 2400, Kind: model.KindMount, Source: model.SourceProc,
		Paths: []string{"cloud:Archive", "/srv/archive"}, Target: "/srv/archive",
		RSS: 72 * mib, Threads: 12, IOAvailable: true,
		ReadRate: float64((step%5)+1) * mib / 2, ReadTotal: uint64(120+step) * mib}
	procs := []model.Process{mount}
	job := model.Job{LogFile: "/srv/logs/backup.log", PID: 2500, Kind: model.KindCopy,
		Path1: "/srv/documents", Path2: "backup:Documents", HaveStats: true,
		Source: model.SourceLog, Stats: model.JobStats{Source: model.SourceLog, Bytes: uint64(step) * 4 * mib, TotalBytes: 48 * mib,
			Transfers: step, TotalTransfers: 12, Speed: 4 * mib, Elapsed: time.Duration(step) * time.Second,
			ETAKnown: true, ETA: time.Duration(lastStep-step) * time.Second},
		Transferring: []model.Transfer{{Name: "report.pdf", Percentage: step * 100 / lastStep,
			Bytes: uint64(step) * mib, BytesKnown: true, Size: 12 * mib, Speed: mib}}}
	unit := model.Unit{Name: "backup.service", ActiveState: "activating", SubState: "start",
		MainPID: 2500, LogFile: job.LogFile, Source: model.SourceSystemd}
	if step < lastStep {
		procs = append(procs, model.Process{PID: 2500, Kind: model.KindCopy, Source: model.SourceProc,
			Paths: []string{job.Path1, job.Path2}, Unit: unit.Name, RSS: 85 * mib, Threads: 8,
			IOAvailable: true, ReadRate: 4 * mib, WriteRate: float64(step%3) * mib,
			ReadTotal: job.Stats.Bytes, WriteTotal: job.Stats.Bytes})
	} else {
		job.PID, job.Finished, job.Outcome = 0, true, "successful"
		job.Transferring = []model.Transfer{}
		unit.MainPID, unit.ActiveState, unit.SubState, unit.Result = 0, "inactive", "dead", "success"
	}
	snapshots := []model.Snapshot{
		{At: at, Source: model.SourceProc, Processes: procs},
		{At: at, Source: model.SourceLog, Jobs: []model.Job{job}},
		{At: at, Source: model.SourceSystemd, Units: []model.Unit{unit}},
		{At: at, Source: model.SourceLocalFS, Caches: []model.CacheDir{{Kind: "vfs", Bytes: 47 * mib, Files: 326}}},
	}
	results := make([]collect.Result, 0, len(snapshots))
	for _, snap := range snapshots {
		results = append(results, collect.Result{Name: string(snap.Source), Source: snap.Source, Snapshot: snap})
	}
	return results
}
