// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"container/heap"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	errNonPositiveHalflife = errors.New("timeout halflife must be positive")

	_ heap.Interface         = &timeoutQueue{}
	_ AdaptiveTimeoutManager = &adaptiveTimeoutManager{}
)

type adaptiveTimeout struct {
	index    int           // Index in the wait queue
	id       ids.ID        // Unique ID of this timeout
	handler  func()        // Function to execute if timed out
	duration time.Duration // How long this timeout was set for
	deadline time.Time     // When this timeout should be fired
	op       message.Op    // Type of this outstanding request
}

type timeoutQueue []*adaptiveTimeout

func (tq timeoutQueue) Len() int           { return len(tq) }
func (tq timeoutQueue) Less(i, j int) bool { return tq[i].deadline.Before(tq[j].deadline) }
func (tq timeoutQueue) Swap(i, j int) {
	tq[i], tq[j] = tq[j], tq[i]
	tq[i].index = i
	tq[j].index = j
}

// Push adds an item to this priority queue. x must have type *adaptiveTimeout
func (tq *timeoutQueue) Push(x interface{}) {
	item := x.(*adaptiveTimeout)
	item.index = len(*tq)
	*tq = append(*tq, item)
}

// Pop returns the next item in this queue
func (tq *timeoutQueue) Pop() interface{} {
	n := len(*tq)
	item := (*tq)[n-1]
	(*tq)[n-1] = nil // make sure the item is freed from memory
	*tq = (*tq)[:n-1]
	return item
}

// AdaptiveTimeoutConfig contains the parameters provided to the
// adaptive timeout manager.
type AdaptiveTimeoutConfig struct {
	InitialTimeout time.Duration `json:"initialTimeout"`
	MinimumTimeout time.Duration `json:"minimumTimeout"`
	MaximumTimeout time.Duration `json:"maximumTimeout"`
	// Timeout is [timeoutCoefficient] * average response time
	// [timeoutCoefficient] must be > 1
	TimeoutCoefficient float64 `json:"timeoutCoefficient"`
	// Larger halflife --> less volatile timeout
	// [timeoutHalfLife] must be positive
	TimeoutHalflife time.Duration `json:"timeoutHalflife"`
}

type AdaptiveTimeoutManager interface {
	// Start the timeout manager.
	// Must be called before any other method.
	// Must only be called once.
	Dispatch()
	// Stop the timeout manager.
	// Must only be called once.
	Stop()
	// Returns the current network timeout duration.
	TimeoutDuration() time.Duration
	// Registers a timeout for the item with the given [id].
	// If the timeout occurs before the item is Removed, [timeoutHandler] is called.
	// Returns the time at which the timeout will fire if it is not first
	// removed by calling [Remove].
	Put(id ids.ID, op message.Op, timeoutHandler func()) time.Time
	// Remove the timeout associated with [id].
	// Its timeout handler will not be called.
	Remove(id ids.ID)
	// ObserveLatency manually registers a response latency.
	// We use this to pretend that it a query to a benched validator
	// timed out when actually, we never even sent them a request.
	ObserveLatency(latency time.Duration)
}

type adaptiveTimeoutManager struct {
	lock sync.Mutex
	// Tells the time. Can be faked for testing.
	clock                            mockable.Clock
	networkTimeoutMetric, avgLatency prometheus.Gauge
	numTimeouts                      prometheus.Counter
	// Averages the response time from all peers
	averager math.Averager
	// Timeout is [timeoutCoefficient] * average response time
	// [timeoutCoefficient] must be > 1
	timeoutCoefficient float64
	minimumTimeout     time.Duration
	maximumTimeout     time.Duration
	currentTimeout     time.Duration // Amount of time before a timeout
	timeoutMap         map[ids.ID]*adaptiveTimeout
	timeoutQueue       timeoutQueue
	timer              *Timer // Timer that will fire to clear the timeouts
}

func NewAdaptiveTimeoutManager(
	config *AdaptiveTimeoutConfig,
	metricsNamespace string,
	metricsRegister prometheus.Registerer,
) (AdaptiveTimeoutManager, error) {
	switch {
	case config.InitialTimeout > config.MaximumTimeout:
		return nil, fmt.Errorf("initial timeout (%s) > maximum timeout (%s)", config.InitialTimeout, config.MaximumTimeout)
	case config.InitialTimeout < config.MinimumTimeout:
		return nil, fmt.Errorf("initial timeout (%s) < minimum timeout (%s)", config.InitialTimeout, config.MinimumTimeout)
	case config.TimeoutCoefficient < 1:
		return nil, fmt.Errorf("timeout coefficient must be >= 1 but got %f", config.TimeoutCoefficient)
	case config.TimeoutHalflife <= 0:
		return nil, errNonPositiveHalflife
	}

	tm := &adaptiveTimeoutManager{
		networkTimeoutMetric: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "current_timeout",
			Help:      "Duration of current network timeout in nanoseconds",
		}),
		avgLatency: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "average_latency",
			Help:      "Average network latency in nanoseconds",
		}),
		numTimeouts: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "timeouts",
			Help:      "Number of timed out requests",
		}),
		minimumTimeout:     config.MinimumTimeout,
		maximumTimeout:     config.MaximumTimeout,
		currentTimeout:     config.InitialTimeout,
		timeoutCoefficient: config.TimeoutCoefficient,
		timeoutMap:         make(map[ids.ID]*adaptiveTimeout),
	}
	tm.timer = NewTimer(tm.timeout)
	tm.averager = math.NewAverager(float64(config.InitialTimeout), config.TimeoutHalflife, tm.clock.Time())

	errs := &wrappers.Errs{}
	errs.Add(metricsRegister.Register(tm.networkTimeoutMetric))
	errs.Add(metricsRegister.Register(tm.avgLatency))
	errs.Add(metricsRegister.Register(tm.numTimeouts))
	return tm, errs.Err
}

func (tm *adaptiveTimeoutManager) TimeoutDuration() time.Duration {
	tm.lock.Lock()
	defer tm.lock.Unlock()
	return tm.currentTimeout
}

func (tm *adaptiveTimeoutManager) Dispatch() { tm.timer.Dispatch() }

func (tm *adaptiveTimeoutManager) Stop() { tm.timer.Stop() }

func (tm *adaptiveTimeoutManager) Put(id ids.ID, op message.Op, timeoutHandler func()) time.Time {
	tm.lock.Lock()
	defer tm.lock.Unlock()
	return tm.put(id, op, timeoutHandler)
}

// Assumes [tm.lock] is held
func (tm *adaptiveTimeoutManager) put(id ids.ID, op message.Op, handler func()) time.Time {
	now := tm.clock.Time()
	tm.remove(id, now)

	timeout := &adaptiveTimeout{
		id:       id,
		handler:  handler,
		duration: tm.currentTimeout,
		deadline: now.Add(tm.currentTimeout),
		op:       op,
	}
	tm.timeoutMap[id] = timeout
	heap.Push(&tm.timeoutQueue, timeout)

	tm.setNextTimeoutTime()
	return timeout.deadline
}

func (tm *adaptiveTimeoutManager) Remove(id ids.ID) {
	tm.lock.Lock()
	defer tm.lock.Unlock()
	tm.remove(id, tm.clock.Time())
}

// Assumes [tm.lock] is held
func (tm *adaptiveTimeoutManager) remove(id ids.ID, now time.Time) {
	timeout, exists := tm.timeoutMap[id]
	if !exists {
		return
	}

	// Observe the response time to update average network response time.
	// Don't include Get requests in calculation, since an adversary
	// can cause you to issue a Get request and then cause it to timeout,
	// increasing your timeout.
	if timeout.op != message.Get {
		timeoutRegisteredAt := timeout.deadline.Add(-1 * timeout.duration)
		latency := now.Sub(timeoutRegisteredAt)
		tm.observeLatencyAndUpdateTimeout(latency, now)
	}

	// Remove the timeout from the map
	delete(tm.timeoutMap, id)

	// Remove the timeout from the queue
	heap.Remove(&tm.timeoutQueue, timeout.index)
}

// Assumes [tm.lock] is not held.
func (tm *adaptiveTimeoutManager) timeout() {
	tm.lock.Lock()
	defer tm.lock.Unlock()
	now := tm.clock.Time()
	for {
		// getNextTimeoutHandler returns nil once there is nothing left to remove
		timeoutHandler := tm.getNextTimeoutHandler(now)
		if timeoutHandler == nil {
			break
		}
		tm.numTimeouts.Inc()

		// Don't execute a callback with a lock held
		tm.lock.Unlock()
		timeoutHandler()
		tm.lock.Lock()
	}
	tm.setNextTimeoutTime()
}

func (tm *adaptiveTimeoutManager) ObserveLatency(latency time.Duration) {
	tm.lock.Lock()
	defer tm.lock.Unlock()
	tm.observeLatencyAndUpdateTimeout(latency, tm.clock.Time())
}

// Assumes [tm.lock] is held
func (tm *adaptiveTimeoutManager) observeLatencyAndUpdateTimeout(latency time.Duration, now time.Time) {
	tm.averager.Observe(float64(latency), now)
	avgLatency := tm.averager.Read()
	tm.currentTimeout = time.Duration(tm.timeoutCoefficient * avgLatency)
	if tm.currentTimeout > tm.maximumTimeout {
		tm.currentTimeout = tm.maximumTimeout
	} else if tm.currentTimeout < tm.minimumTimeout {
		tm.currentTimeout = tm.minimumTimeout
	}
	// Update the metrics
	tm.networkTimeoutMetric.Set(float64(tm.currentTimeout))
	tm.avgLatency.Set(avgLatency)
}

// Returns the handler function associated with the next timeout.
// If there are no timeouts, or if the next timeout is after [now],
// returns nil.
// Assumes [tm.lock] is held
func (tm *adaptiveTimeoutManager) getNextTimeoutHandler(now time.Time) func() {
	if tm.timeoutQueue.Len() == 0 {
		return nil
	}

	nextTimeout := tm.timeoutQueue[0]
	if nextTimeout.deadline.After(now) {
		return nil
	}
	tm.remove(nextTimeout.id, now)
	return nextTimeout.handler
}

// Calculate the time of the next timeout and set
// the timer to fire at that time.
func (tm *adaptiveTimeoutManager) setNextTimeoutTime() {
	if tm.timeoutQueue.Len() == 0 {
		// There are no pending timeouts
		tm.timer.Cancel()
		return
	}

	now := tm.clock.Time()
	nextTimeout := tm.timeoutQueue[0]
	timeToNextTimeout := nextTimeout.deadline.Sub(now)
	tm.timer.SetTimeoutIn(timeToNextTimeout)
}
