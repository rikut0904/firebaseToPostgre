package migration

import (
	"firebaseToPostgre/internal/database"
	"firebaseToPostgre/internal/firebase"
)

// CollectionMapping defines a single collection-to-table mapping.
type CollectionMapping struct {
	Name             string          `json:"name"`
	SourceCollection string          `json:"sourceCollection"`
	TargetTable      string          `json:"targetTable"`
	CreateTable      bool            `json:"createTable"`
	Columns          []ColumnMapping `json:"columns"`
}

// ColumnMapping defines how a Firebase field maps to a PostgreSQL column.
type ColumnMapping struct {
	Source     string `json:"source"`
	Target     string `json:"target"`
	Type       string `json:"type"`
	PrimaryKey bool   `json:"primaryKey"`
	Nullable   bool   `json:"nullable"`
}

// Options holds migration runtime options.
type Options struct {
	BatchSize     int    `json:"batchSize"`
	Workers       int    `json:"workers"`
	ErrorStrategy string `json:"errorStrategy"` // "continue" or "stop"
	DryRun        bool   `json:"dryRun"`
}

// Request is the migration request payload.
type Request struct {
	Firebase    firebase.ConnInfo `json:"firebase"`
	Destination database.ConnInfo `json:"destination"`
	Mappings    []CollectionMapping `json:"mappings"`
	Options     Options             `json:"options"`
}

// Status represents the overall migration status.
type Status struct {
	Running      bool          `json:"running"`
	Overall      int           `json:"overallPercent"`
	ElapsedSec   int64         `json:"elapsedSeconds"`
	CurrentPhase string        `json:"currentPhase"`
	LogMessage   string        `json:"logMessage"`
	Tables       []TableStatus `json:"tables"`
	FailedTables []FailedTable `json:"failedTables,omitempty"`
}

// TableStatus represents progress for a single table.
type TableStatus struct {
	Name             string `json:"name"`
	SourceCollection string `json:"sourceCollection"`
	TargetTable      string `json:"targetTable"`
	Phase            string `json:"phase"`
	Total            int64  `json:"total"`
	Read             int64  `json:"read"`
	Written          int64  `json:"written"`
	Skipped          int64  `json:"skipped"`
	Errors           int64  `json:"errors"`
	Percent          int    `json:"percent"`
	Message          string `json:"message,omitempty"`
}

// FailedTable captures a failed table mapping.
type FailedTable struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}
