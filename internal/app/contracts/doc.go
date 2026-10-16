// Package contracts is the Go side of the Go–TypeScript contract
// (docs/decisions/api-contract.md): the DTOs that cross Wails, their identifiers and units,
// AppError, and the validation of requests.
//
// Invariants every type here keeps:
//   - Only these own structures cross the bridge: no engine objects, channels or mutable pointers
//     into services (AI-05). A pointer field only means "this value may be null".
//   - Every 64-bit quantity (bytes, bytes per second, seconds, revisions) is a decimal string on
//     the wire, so JavaScript never rounds it; the TypeScript side reads it as a bigint.
//   - null means "unknown" and is never written as zero: a total of null is "metadata not known
//     yet", a total of "0" is "known to be empty".
//   - Every key is always present. The frontend generates operation IDs; Go generates torrent,
//     pending-import and runtime IDs.
//   - Requests are decoded field by field and validated here whatever the UI checked (STACK-01):
//     a missing, null, unknown or badly typed field is request.invalid naming that field.
//
// Shared examples in testdata/ are read by these tests and by the TypeScript bridge tests.
package contracts
