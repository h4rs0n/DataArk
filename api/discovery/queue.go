package discovery

import "sync"

var (
	defaultJobQueueMu sync.RWMutex
	defaultJobQueue   DiscoveryJobEnqueuer
)

// SetJobQueue 注入发现流水线队列。装配层在 jobqueue.Start 成功后调用，stop 时传入 nil。
func SetJobQueue(queue DiscoveryJobEnqueuer) {
	defaultJobQueueMu.Lock()
	defer defaultJobQueueMu.Unlock()
	defaultJobQueue = queue
}

// jobQueue 返回当前发现队列；未注入时 available 为 false，入队应视为 no-op。
func jobQueue() (DiscoveryJobEnqueuer, bool) {
	defaultJobQueueMu.RLock()
	defer defaultJobQueueMu.RUnlock()
	return defaultJobQueue, defaultJobQueue != nil
}
