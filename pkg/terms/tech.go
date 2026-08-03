package terms

// DefaultTechTerms returns a list of common technology, architecture, programming,
// and engineering terms commonly used in technical interviews.
func DefaultTechTerms() []string {
	return []string{
		// Languages & Runtimes
		"Go", "Golang", "TypeScript", "JavaScript", "Python", "Rust", "C++", "Java",
		"Kotlin", "Swift", "Node.js", "Bun", "Deno",

		// Frontend & Frameworks
		"React", "Vue", "Next.js", "Svelte", "Angular", "Tailwind", "Vite", "WebAssembly",

		// Cloud & Infrastructure
		"Kubernetes", "K8s", "Docker", "AWS", "GCP", "Azure", "Terraform", "Helm",
		"Serverless", "Lambda", "Cloudflare", "Microservices", "CI/CD", "GitHub Actions",
		"Envoy", "Nginx", "Istio",

		// Databases & Storage
		"PostgreSQL", "Postgres", "MySQL", "Redis", "MongoDB", "SQLite", "Cassandra",
		"DynamoDB", "Kafka", "RabbitMQ", "Vector DB", "Elasticsearch", "ClickHouse",

		// Architecture & Protocols
		"REST", "GraphQL", "gRPC", "Protobuf", "WebSockets", "OAuth", "JWT",
		"Distributed Systems", "Event-Driven", "Concurrency", "Goroutine", "Channel",
		"Garbage Collection", "Scalability", "Observability", "OpenTelemetry",
		"Prometheus", "Grafana", "Tracing", "Metrics",

		// Testing, DevOps & Methodologies
		"Unit Test", "Integration Test", "Agile", "Scrum", "System Design",
		"Code Review", "GitOps", "Rate Limiting", "Circuit Breaker",
	}
}
