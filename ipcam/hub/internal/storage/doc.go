// Package storage is the hub's storage driver interface (§9.1, ARH-05): the
// Destination that each provider's driver implements, the Caps it declares,
// its typed errors with their §7.7 reasons, and Digest, which hashes local
// bytes the way a provider does.
//
// The rest of the hub works only with the declared Caps, and a field that a
// driver leaves unset claims the least (P24). Every method takes ctx first
// (P11). Drivers do not verify an upload: Commit returns the provider's size
// and hash, and the outbox compares them with the size and the Digest of the
// bytes it uploaded (P34).
package storage
