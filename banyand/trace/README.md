# BanyanDB Trace Package

This package implements trace-based storage for BanyanDB, following the established patterns from the `stream` and `measure` packages.

## Package Structure

```
banyand/trace/
├── trace.go              # Core trace interface and implementation
├── metadata.go           # Schema repository and suppliers
├── svc_standalone.go     # Standalone service implementation  
├── svc_liaison.go        # Liaison service implementation
└── README.md             # This file
```

## Key Components

### Interfaces

- **Service**: Main service interface combining `run.PreRunner`, `run.Config`, `run.Service`, and `Query`
- **Query**: Allows retrieving traces with methods like `LoadGroup`, `Trace`, and `GetRemovalSegmentsTimeRange`
- **Trace**: Allows inspecting trace details with methods like `GetSchema`, `GetIndexRules`, and `Query`

### Services

- **Standalone Service**: Implements `ROLE_DATA` for local trace operations
- **Liaison Service**: Implements `ROLE_LIAISON` for distributed trace operations

### Features Implemented

- [x] Core trace interfaces following the established patterns
- [x] Schema repository with suppliers for both standalone and liaison services
- [x] Metadata CRUD operations (Create, Read, Update, Delete)
- [x] Trace validation
- [x] Integration with the metadata repository
- [x] Support for trace-specific query options and results

### Features Not Yet Implemented

- [ ] Pipeline listeners (intentionally omitted as per plan)
- [ ] OpenDB method in suppliers (intentionally omitted as per plan)
- [ ] Full trace indexing support (trace schema doesn't have IndexRuleBinding yet)
- [ ] Trace partitioning support
- [ ] Complete trace query implementation

## Configuration

### Standalone Service Flags

- `--trace-root-path` (default: "/tmp")
- `--trace-data-path` (optional, derived from root-path if not set)
- `--trace-flush-timeout` (default: 1 second)
- `--trace-max-disk-usage-percent` (default: 95)
- `--trace-max-file-snapshot-num` (default: 2)

### Liaison Service Flags

- `--trace-root-path` (default: "/tmp")
- `--trace-data-path` (optional, derived from root-path if not set)
- `--trace-flush-timeout` (default: 1 second)
- `--trace-max-disk-usage-percent` (default: 95)

## Integration Points

### Metadata Integration

- Added `TraceRegistry()` method to the metadata repository interface
- Added `KindTrace` to the schema kind enumeration
- Implemented trace CRUD operations in the schema package
- Added trace validation function

### Storage Integration

- Uses `storage.TSDB` for time-series data storage
- Implements proper schema management and TTL handling

### Query Integration

- Added `TraceQueryOptions` and `TraceQueryResult` to the model package
- Supports trace-specific query patterns

## Schema Definition

Traces are defined using the `databasev1.Trace` proto message with:

- `metadata`: Identity of the trace resource
- `tags`: Specification of trace tags  
- `trace_id_tag_name`: Name of the tag storing the trace ID
- `timestamp_tag_name`: Name of the tag storing the timestamp

## Usage

### Creating Services

```go
// Standalone service
svc, err := trace.StandaloneService(ctx)

// Liaison service  
svc, err := trace.LiaisonService(ctx)
```

### Querying Traces

```go
// Get a trace
trace, err := svc.Trace(metadata)

// Query trace data
result, err := trace.Query(ctx, opts)
```

## Notes

- This implementation follows the trace-plan.md specifications
- No pipeline listeners are implemented as requested
- Suppliers do not implement OpenDB method as requested
- Index rule binding support is placeholder (returns nil) until trace indexing is fully implemented
- Trace partitioning support is placeholder until needed