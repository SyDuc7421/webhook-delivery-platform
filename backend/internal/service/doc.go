// Package service implements use cases: ingest (event + outbox in one transaction),
// dispatch (fan-out to subscribed endpoints), deliver, retry, replay and the
// dashboard CRUD. Transactions, org scoping and business rules live here.
// It depends on domain only.
package service
