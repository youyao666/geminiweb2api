package main

import (
	"sync/atomic"
	"time"
)

type Metrics struct {
	TotalRequests   uint64    `json:"total_requests"`
	SuccessRequests uint64    `json:"success_requests"`
	FailedRequests  uint64    `json:"failed_requests"`
	InputTokens     uint64    `json:"input_tokens"`
	OutputTokens    uint64    `json:"output_tokens"`
	StartTime       time.Time `json:"-"`
	RecentRequests  []int64   `json:"-"`
}

var metrics = &Metrics{
	StartTime:      time.Now(),
	RecentRequests: make([]int64, 0),
}

func (m *Metrics) AddRequest(success bool, inputTokens, outputTokens int) {
	atomic.AddUint64(&m.TotalRequests, 1)
	if success {
		atomic.AddUint64(&m.SuccessRequests, 1)
	} else {
		atomic.AddUint64(&m.FailedRequests, 1)
	}
	atomic.AddUint64(&m.InputTokens, uint64(inputTokens))
	atomic.AddUint64(&m.OutputTokens, uint64(outputTokens))
	m.RecentRequests = append(m.RecentRequests, time.Now().Unix())
}

func (m *Metrics) GetRPM() float64 {
	now := time.Now().Unix()
	oneMinuteAgo := now - 60
	count := 0
	var recent []int64
	for _, t := range m.RecentRequests {
		if t >= oneMinuteAgo {
			count++
			recent = append(recent, t)
		}
	}
	m.RecentRequests = recent
	return float64(count)
}
