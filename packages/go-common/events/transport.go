package events

// Transport-backed Publisher implementations live in this file as stubs
// so Phase-2 can drop in real NATS / Kafka clients without changing the
// service code.
//
// The pattern: each transport adapter implements Publisher; services
// receive whichever one was wired by main(). The InProc bus is the
// default for dev / single-process deployments.

// Want NATS? Implement:
//
//   type NATS struct{ nc *nats.Conn; subj func(string)string }
//   func (n *NATS) Publish(ctx, env) error { ... json marshal, n.nc.Publish(subj, body) }
//
// Want Kafka? Implement:
//
//   type Kafka struct{ writer *kafka.Writer }
//   func (k *Kafka) Publish(ctx, env) error { ... }
//
// Both can use envelope.TenantID for partitioning and envelope.Type for
// topic/subject. Subscribers on the consuming side translate transport
// messages back into Envelope and dispatch to handlers.
