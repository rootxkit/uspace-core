// Package sources is the source-control model (spec 04 §3.6
// source/control; predecessor U-15; LESSONS B-09, B-10, B-11).
//
// A State is a list of switches by source type and by instance, a
// DefaultDeny flag, and the Version and Epoch that order states. Query
// answers "is this source enabled, and if not why" (type, instance or
// default_deny): a type switched off disables every instance; an instance
// switched off stays off under a type that is on; an explicit instance row
// that is on overrides default deny; a whole-type question is decided by
// the type row only.
//
// A Follower holds the last State applied. Within an epoch it takes only a
// strictly higher version; a new epoch (a restored database) is taken
// whatever its version. With no state it enables everything: a follower
// never fails closed (B-09). It counts applied, ignored_older_version and
// new_epoch.
//
// For callers (B-10): refuse a connection from a disabled source with 503
// and Retry-After, never 401 or 403, which clients treat as fatal; close an
// open session with 1013; count the refusal. The reason (Why) travels with
// the refusal and with the alerts cleared as source_disabled (B-11).
//
// Vectors: vectors/testdata/source_control.json (8). Owned by WP-4.
package sources
