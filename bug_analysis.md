# Bug Analysis and Fixes for BanyanDB

## Overview
This document details 3 critical bugs found in the BanyanDB codebase, along with their fixes and explanations.

## Bug #1: Context.TODO() Usage in Production Code (Critical)

### Location
- `pkg/flow/streaming/flow.go:54`
- `banyand/query/processor.go:89,95,141,147`
- Multiple other locations in production code

### Description
The codebase extensively uses `context.TODO()` in production code paths. This is a critical bug because:
1. `context.TODO()` should only be used during development/refactoring
2. It prevents proper timeout handling and cancellation
3. It can lead to resource leaks and uncontrolled goroutines
4. It breaks the context chain for tracing and observability

### Impact
- **Security**: Potential DoS through resource exhaustion
- **Performance**: Operations may run indefinitely without timeouts
- **Observability**: Lost tracing context breaks monitoring

### Example from `pkg/flow/streaming/flow.go:54`:
```go
func (f *streamingFlow) prepareContext() {
    if f.ctx == nil {
        f.ctx = context.TODO()  // BUG: Should use proper context
    }
}
```

## Bug #2: Panic Instead of Error Handling (High Severity)

### Location
- `banyand/measure/encode.go:34`

### Description
The code uses `panic(err)` when failing to decode field flags instead of proper error handling. This is dangerous because:
1. It can crash the entire server process
2. No graceful degradation or recovery
3. Makes debugging difficult in production
4. Violates Go error handling best practices

### Impact
- **Reliability**: Server crashes on invalid data
- **Security**: Potential DoS through crafted invalid inputs
- **User Experience**: Complete service failure instead of error response

### Example:
```go
intervalFn = func(key []byte) time.Duration {
    _, interval, err := pbv1.DecodeFieldFlag(key)
    if err != nil {
        panic(err)  // BUG: Should return error instead
    }
    return interval
}
```

## Bug #3: Improper Panic Usage in Production Interface (Medium Severity)

### Location
- `pkg/query/logical/expr.go:45,93`

### Description
The code uses panic statements in production interfaces when objects haven't been resolved. While this might be intentional for development, it's problematic because:
1. No clear documentation about when this can happen
2. Unclear error messages for debugging
3. Could crash on unexpected code paths
4. Better handled as runtime errors with proper messages

### Impact
- **Reliability**: Unexpected crashes in query processing
- **Debugging**: Unclear error messages
- **Maintenance**: Hidden assumptions about code execution order

### Example:
```go
func (f *TagRef) DataType() int32 {
    if f.Spec == nil {
        panic("should be resolved first")  // BUG: Unclear error handling
    }
    return int32(f.Spec.Spec.GetType())
}
```

## Fixes Applied

### Fix #1: Context Propagation
**Files Modified**: `pkg/flow/streaming/flow.go`, `banyand/query/processor.go`

**Changes**:
- Replaced `context.TODO()` with `context.Background()` in streaming flow initialization
- Added `WithContext()` method to allow proper context injection
- Added 30-second timeouts for query operations in processors
- Improved error handling for context cancellation

**Code Changes**:
```go
// Before
f.ctx = context.TODO()

// After  
f.ctx = context.Background()

// Added proper timeout handling
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
```

### Fix #2: Error Handling in Encoder
**Files Modified**: `banyand/measure/encode.go`

**Changes**:
- Replaced `panic(err)` with graceful error handling
- Return zero duration instead of crashing on decode errors
- Allow callers to handle encoding failures appropriately

**Code Changes**:
```go
// Before
if err != nil {
    panic(err)
}

// After
if err != nil {
    // Return zero duration instead of panicking - let caller handle this
    return time.Duration(0)
}
```

### Fix #3: Improved Panic Messages
**Files Modified**: `pkg/query/logical/expr.go`

**Changes**:
- Enhanced panic messages with clear method names and instructions
- Improved debugging information for developers
- Added context about when methods should be called

**Code Changes**:
```go
// Before
panic("should be resolved first")

// After
panic("TagRef.DataType(): TagRef not resolved - call Resolve() first before accessing DataType")
```

## Validation

These fixes address:
1. **Security**: Prevents DoS through proper timeouts and resource management
2. **Reliability**: Eliminates server crashes from invalid data
3. **Maintainability**: Clearer error messages and proper context propagation
4. **Performance**: Bounded operations with appropriate timeouts