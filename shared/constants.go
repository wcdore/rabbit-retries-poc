package shared

// Retry Configuration
const (
	MaxRetries = 4
)

// Queue and Exchange Configuration
const (
	WorkingExchange = "working_exchange"
	WorkQueue       = "work_queue"
	WorkRoutingKey  = "work"
	DirectExchange  = "direct"
)

// Content Types
const (
	JSONContentType = "application/json"
)


// Application Configuration
const (
	DefaultMessageCount = 20
)
