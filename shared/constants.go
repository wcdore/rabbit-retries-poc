package shared

// Retry Configuration
const (
	MaxRetries = 4
)

// Queue and Exchange Configuration
const (
	ExchangeNameTransaction = "TRANSACTION"
	ExchangeTypeTopic       = "topic"

	// Queue names
	QueueOrderLedger  = "order-ledger"
	QueueDataConsumer = "data-consumer"

	// Routing keys
	TopicTransactionProcessed             = "transaction.processed"               // For new messages
	TopicTransactionProcessedOrderLedger  = "transaction.processed.order-ledger"  // For order-ledger retries
	TopicTransactionProcessedDataConsumer = "transaction.processed.data-consumer" // For data-consumer retries
)

// Content Types
const (
	JSONContentType = "application/json"
)

// Application Configuration
const (
	DefaultMessageCount = 20
)
