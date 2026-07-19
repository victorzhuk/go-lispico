package runtime

import (
	"sync"
	"sync/atomic"
	"time"
)

type EvalEvent struct {
	Source   string
	Duration time.Duration
	Error    error
}

type PluginCallEvent struct {
	Plugin   string
	Function string
	Duration time.Duration
}

type Stats struct {
	evalCount      atomic.Int64
	errorCount     atomic.Int64
	totalEvalNs    atomic.Int64
	activePlugins  atomic.Int64
	startTime      time.Time
	pluginCallCnts sync.Map // string -> *atomic.Int64
}

func newStats() *Stats {
	return &Stats{
		startTime: time.Now(),
	}
}

type EngineStats struct {
	TotalEvals       int64
	TotalErrors      int64
	TotalEvalNs      int64
	AvgEvalNs        int64
	PluginCallCounts map[string]int64
	ActivePlugins    int
	Uptime           time.Duration
}

func (s *Stats) Snapshot() EngineStats {
	pluginCounts := make(map[string]int64)
	s.pluginCallCnts.Range(func(k, v any) bool {
		pluginCounts[k.(string)] = v.(*atomic.Int64).Load()
		return true
	})

	totalEvals := s.evalCount.Load()
	var avgEvalNs int64
	if totalEvals > 0 {
		avgEvalNs = s.totalEvalNs.Load() / totalEvals
	}

	return EngineStats{
		TotalEvals:       totalEvals,
		TotalErrors:      s.errorCount.Load(),
		TotalEvalNs:      s.totalEvalNs.Load(),
		AvgEvalNs:        avgEvalNs,
		PluginCallCounts: pluginCounts,
		ActivePlugins:    int(s.activePlugins.Load()),
		Uptime:           time.Since(s.startTime),
	}
}

func (s *Stats) recordEval(dur time.Duration, err error) {
	s.evalCount.Add(1)
	s.totalEvalNs.Add(int64(dur))
	if err != nil {
		s.errorCount.Add(1)
	}
}

func (s *Stats) countPluginCall(name string) {
	if v, ok := s.pluginCallCnts.Load(name); ok {
		v.(*atomic.Int64).Add(1)
		return
	}
	actual, _ := s.pluginCallCnts.LoadOrStore(name, new(atomic.Int64))
	actual.(*atomic.Int64).Add(1)
}

func (s *Stats) incPlugins() {
	s.activePlugins.Add(1)
}

func (s *Stats) decPlugins() {
	s.activePlugins.Add(-1)
}
