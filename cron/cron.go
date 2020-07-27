// Copyright © 2019 Martin Tournoij – This file is part of GoatCounter and
// published under the terms of a slightly modified EUPL v1.2 license, which can
// be found in the LICENSE file or at https://license.goatcounter.com

// Package cron schedules jobs.
package cron

import (
	"context"
	"strings"
	"time"

	"zgo.at/goatcounter/bgrun"
	"zgo.at/zdb"
	"zgo.at/zlog"
	"zgo.at/zstd/zruntime"
	"zgo.at/zstd/zsync"
)

type task struct {
	fun    func(context.Context) error
	period time.Duration
}

var tasks = []task{
	{PersistAndStat, 10 * time.Second},
	{DataRetention, 1 * time.Hour},
	{renewACME, 2 * time.Hour},
	{vacuumDeleted, 12 * time.Hour},
	{oldExports, 1 * time.Hour},
	{sessions, 1 * time.Minute},
}

var stopped = zsync.NewAtomicInt(0)

// RunOnce runs all tasks once and returns.
func RunOnce(db zdb.DB) {
	ctx := zdb.With(context.Background(), db)
	l := zlog.Module("cron")
	for _, t := range tasks {
		err := t.fun(ctx)
		if err != nil {
			l.Error(err)
		}
	}
}

// RunBackground runs tasks in the background according to the given schedule.
func RunBackground(db zdb.DB) {
	ctx := zdb.With(context.Background(), db)
	l := zlog.Module("cron")

	for _, t := range tasks {
		go func(t task) {
			defer zlog.Recover()

			for {
				time.Sleep(t.period)
				if stopped.Value() == 1 {
					return
				}

				f := strings.Replace(zruntime.FuncName(t.fun), "zgo.at/goatcounter/cron.", "", 1)
				bgrun.Run("cron:"+f, func() {
					err := t.fun(ctx)
					if err != nil {
						l.Error(err)
					}
					time.Sleep(4 * time.Second)
				})
			}
		}(t)
	}
}

// Wait for all running tasks to finish and then run all tasks for consistency
// on shutdown.
func Wait(db zdb.DB) {
	stopped.Set(1)
	ctx := zdb.With(context.Background(), db)
	for _, t := range tasks {
		err := t.fun(ctx)
		if err != nil {
			zlog.Module("cron").Error(err)
		}
	}
}
