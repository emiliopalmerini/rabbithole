package tui

type Config struct {
	RabbitMQURL string
	Exchange    string
	RoutingKey  string
	QueueName   string
	ProtoPath   string
	ShowVersion bool
	Durable     bool
}
