// Package vectors loads the knowledge vectors of uspace-lab
// (knowledge/vectors/*.json, vendored at a pinned commit under testdata/)
// and gives tests a strict decoder, owner filtering and tolerance helpers.
//
// The 16 files pin the safety behaviour of every judgement in this module
// (00 §6 hard rule). Each package runs its own file(s) in a *_test.go named
// vectors_test.go; the manifest in manifest.go says which package owns
// which file and how many cases it holds, and TestManifest fails when a
// file is missing, has a different case count, or was edited by hand
// (SHA256SUMS). The copy is refreshed only by scripts/sync-vectors.sh,
// which records the uspace-lab commit in testdata/VERSION.
//
// uspace-core runs every case regardless of its owner list (it is the
// shared implementation). A system repository runs RunOwned with its own
// code to execute only the cases that name it.
//
// Comparison rules (knowledge/README.md): times with Equal, never ==;
// floats within the file's tolerance, "exact" compared exactly; integers
// and strings exactly; null in expected is a nil pointer, never a zero
// value; renamed fields fail because decoding disallows unknown fields.
package vectors
