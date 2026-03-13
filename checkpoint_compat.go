package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

type checkpointCompatibility struct {
	Fingerprint string                          `json:"fingerprint,omitempty"`
	Summary     *checkpointCompatibilitySummary `json:"summary,omitempty"`
}

type checkpointCompatibilitySummary struct {
	SourceType           string                         `json:"source_type"`
	SourceDBName         string                         `json:"source_db_name,omitempty"`
	SourceSchema         string                         `json:"source_schema,omitempty"`
	TargetSchema         string                         `json:"target_schema"`
	SourceSnapshotMode   string                         `json:"source_snapshot_mode"`
	SnakeCaseIdentifiers bool                           `json:"snake_case_identifiers"`
	SchemaOnly           bool                           `json:"schema_only"`
	DataOnly             bool                           `json:"data_only"`
	ChunkSize            int64                          `json:"chunk_size"`
	TypeMapping          TypeMappingConfig              `json:"type_mapping"`
	Hooks                []checkpointCompatibilityHook  `json:"hooks,omitempty"`
	Tables               []checkpointCompatibilityTable `json:"tables,omitempty"`
}

type checkpointCompatibilityHook struct {
	Phase  string `json:"phase"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type checkpointCompatibilityTable struct {
	SourceName string `json:"source_name"`
	PGName     string `json:"pg_name"`
	ChunkKey   string `json:"chunk_key,omitempty"`
	TableHash  string `json:"table_hash"`
}

func buildCheckpointCompatibility(cfg *MigrationConfig, schema *Schema, src SourceDB, sourceDBName string) (checkpointCompatibility, error) {
	summary := checkpointCompatibilitySummary{
		SourceType:           cfg.Source.Type,
		SourceDBName:         sourceDBName,
		SourceSchema:         cfg.Source.SourceSchema,
		TargetSchema:         cfg.Schema,
		SourceSnapshotMode:   cfg.SourceSnapshotMode,
		SnakeCaseIdentifiers: cfg.SnakeCaseIdentifiers,
		SchemaOnly:           cfg.SchemaOnly,
		DataOnly:             cfg.DataOnly,
		ChunkSize:            cfg.ChunkSize,
		TypeMapping:          effectiveTypeMapping(cfg),
	}

	hooks, err := checkpointCompatibilityHooks(cfg)
	if err != nil {
		return checkpointCompatibility{}, err
	}
	summary.Hooks = hooks

	tables, err := checkpointCompatibilityTables(schema, src)
	if err != nil {
		return checkpointCompatibility{}, err
	}
	summary.Tables = tables

	fingerprint, err := checkpointCompatibilityFingerprint(summary)
	if err != nil {
		return checkpointCompatibility{}, err
	}
	return checkpointCompatibility{
		Fingerprint: fingerprint,
		Summary:     &summary,
	}, nil
}

func checkpointCompatibilityHooks(cfg *MigrationConfig) ([]checkpointCompatibilityHook, error) {
	type phaseHooks struct {
		phase string
		files []string
	}
	phases := []phaseHooks{
		{phase: "before_data", files: cfg.Hooks.BeforeData},
		{phase: "after_data", files: cfg.Hooks.AfterData},
		{phase: "before_fk", files: cfg.Hooks.BeforeFk},
		{phase: "after_all", files: cfg.Hooks.AfterAll},
	}

	var hooks []checkpointCompatibilityHook
	for _, phase := range phases {
		for _, file := range phase.files {
			path := cfg.resolvePath(file)
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("hash hook %s (%s): %w", phase.phase, file, err)
			}
			sum := sha256.Sum256(data)
			hooks = append(hooks, checkpointCompatibilityHook{
				Phase:  phase.phase,
				Path:   file,
				SHA256: hex.EncodeToString(sum[:]),
			})
		}
	}

	sort.Slice(hooks, func(i, j int) bool {
		if hooks[i].Phase != hooks[j].Phase {
			return hooks[i].Phase < hooks[j].Phase
		}
		return hooks[i].Path < hooks[j].Path
	})

	return hooks, nil
}

func checkpointCompatibilityTables(schema *Schema, src SourceDB) ([]checkpointCompatibilityTable, error) {
	if schema == nil {
		return nil, nil
	}

	tables := make([]checkpointCompatibilityTable, 0, len(schema.Tables))
	for _, table := range schema.Tables {
		hash, err := hashCheckpointTable(table)
		if err != nil {
			return nil, fmt.Errorf("hash table %s: %w", table.SourceName, err)
		}

		var chunkKey string
		if key := chunkKeyForTable(table, src); key != nil {
			chunkKey = key.SourceColumn
		}

		tables = append(tables, checkpointCompatibilityTable{
			SourceName: table.SourceName,
			PGName:     table.PGName,
			ChunkKey:   chunkKey,
			TableHash:  hash,
		})
	}

	sort.Slice(tables, func(i, j int) bool {
		return tables[i].SourceName < tables[j].SourceName
	})

	return tables, nil
}

func hashCheckpointTable(table Table) (string, error) {
	type checkpointColumn struct {
		SourceName           string  `json:"source_name"`
		PGName               string  `json:"pg_name"`
		DataType             string  `json:"data_type"`
		ColumnType           string  `json:"column_type"`
		CharMaxLen           int64   `json:"char_max_len"`
		Precision            int64   `json:"precision"`
		Scale                int64   `json:"scale"`
		Nullable             bool    `json:"nullable"`
		Default              *string `json:"default,omitempty"`
		Extra                string  `json:"extra,omitempty"`
		GenerationExpression string  `json:"generation_expression,omitempty"`
		OrdinalPos           int     `json:"ordinal_pos"`
		Charset              string  `json:"charset,omitempty"`
		Collation            string  `json:"collation,omitempty"`
	}
	type checkpointTable struct {
		SourceName string             `json:"source_name"`
		PGName     string             `json:"pg_name"`
		Columns    []checkpointColumn `json:"columns"`
		PrimaryKey []string           `json:"primary_key,omitempty"`
	}

	snapshot := checkpointTable{
		SourceName: table.SourceName,
		PGName:     table.PGName,
		Columns:    make([]checkpointColumn, 0, len(table.Columns)),
	}
	if table.PrimaryKey != nil {
		snapshot.PrimaryKey = append(snapshot.PrimaryKey, table.PrimaryKey.Columns...)
	}
	for _, col := range table.Columns {
		snapshot.Columns = append(snapshot.Columns, checkpointColumn{
			SourceName:           col.SourceName,
			PGName:               col.PGName,
			DataType:             col.DataType,
			ColumnType:           col.ColumnType,
			CharMaxLen:           col.CharMaxLen,
			Precision:            col.Precision,
			Scale:                col.Scale,
			Nullable:             col.Nullable,
			Default:              col.Default,
			Extra:                col.Extra,
			GenerationExpression: col.GenerationExpression,
			OrdinalPos:           col.OrdinalPos,
			Charset:              col.Charset,
			Collation:            col.Collation,
		})
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func checkpointCompatibilityFingerprint(summary checkpointCompatibilitySummary) (string, error) {
	data, err := json.Marshal(summary)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func validateCheckpointCompatibility(path string, state *CheckpointState, expected checkpointCompatibility) error {
	if state == nil || expected.Fingerprint == "" {
		return nil
	}
	if state.Version < checkpointVersion {
		return fmt.Errorf("checkpoint %s was created by an older pgferry version and cannot be resumed safely; delete %s and rerun the migration", path, path)
	}
	if state.Compatibility == nil || state.Compatibility.Fingerprint == "" || state.Compatibility.Summary == nil {
		return fmt.Errorf("checkpoint %s is missing resume compatibility metadata and cannot be resumed safely; delete %s and rerun the migration", path, path)
	}
	if expected.Summary == nil {
		return nil
	}
	if state.Compatibility.Fingerprint == expected.Fingerprint {
		return nil
	}

	var reasons []string
	saved := *state.Compatibility.Summary
	current := *expected.Summary

	if saved.SourceType != current.SourceType {
		reasons = append(reasons, fmt.Sprintf("source type changed: was %q, now %q", saved.SourceType, current.SourceType))
	}
	if saved.SourceDBName != current.SourceDBName {
		reasons = append(reasons, fmt.Sprintf("source database changed: was %q, now %q", saved.SourceDBName, current.SourceDBName))
	}
	if saved.SourceSchema != current.SourceSchema {
		reasons = append(reasons, fmt.Sprintf("source schema changed: was %q, now %q", saved.SourceSchema, current.SourceSchema))
	}
	if saved.TargetSchema != current.TargetSchema {
		reasons = append(reasons, fmt.Sprintf("target schema changed: was %q, now %q", saved.TargetSchema, current.TargetSchema))
	}
	if saved.SourceSnapshotMode != current.SourceSnapshotMode {
		reasons = append(reasons, fmt.Sprintf("source snapshot mode changed: was %q, now %q", saved.SourceSnapshotMode, current.SourceSnapshotMode))
	}
	if saved.ChunkSize != current.ChunkSize {
		reasons = append(reasons, fmt.Sprintf("chunk_size changed: was %d, now %d", saved.ChunkSize, current.ChunkSize))
	}
	if saved.SnakeCaseIdentifiers != current.SnakeCaseIdentifiers {
		reasons = append(reasons, fmt.Sprintf("snake_case_identifiers changed: was %t, now %t", saved.SnakeCaseIdentifiers, current.SnakeCaseIdentifiers))
	}
	if saved.SchemaOnly != current.SchemaOnly || saved.DataOnly != current.DataOnly {
		reasons = append(reasons, fmt.Sprintf("migration mode changed: was schema_only=%t data_only=%t, now schema_only=%t data_only=%t", saved.SchemaOnly, saved.DataOnly, current.SchemaOnly, current.DataOnly))
	}
	reasons = append(reasons, checkpointTypeMappingDiff(saved.TypeMapping, current.TypeMapping)...)

	reasons = append(reasons, checkpointHookCompatibilityDiff(saved.Hooks, current.Hooks)...)
	reasons = append(reasons, checkpointTableCompatibilityDiff(saved.Tables, current.Tables)...)
	if len(reasons) == 0 {
		reasons = append(reasons, "migration compatibility fingerprint changed")
	}

	const maxReasons = 8
	if len(reasons) > maxReasons {
		extra := len(reasons) - maxReasons
		reasons = append(reasons[:maxReasons], fmt.Sprintf("%d more compatibility differences omitted", extra))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "checkpoint %s is incompatible with the current migration:\n", path)
	for _, reason := range reasons {
		fmt.Fprintf(&b, "  - %s\n", reason)
	}
	fmt.Fprintf(&b, "Delete %s and rerun the migration from scratch, or restore the original config/schema that created the checkpoint.", path)
	return errors.New(b.String())
}

func checkpointHookCompatibilityDiff(saved, current []checkpointCompatibilityHook) []string {
	savedByID := make(map[string]checkpointCompatibilityHook, len(saved))
	currentByID := make(map[string]checkpointCompatibilityHook, len(current))

	for _, hook := range saved {
		savedByID[checkpointHookID(hook)] = hook
	}
	for _, hook := range current {
		currentByID[checkpointHookID(hook)] = hook
	}

	var reasons []string
	for id, hook := range savedByID {
		currentHook, ok := currentByID[id]
		if !ok {
			reasons = append(reasons, fmt.Sprintf("%s hook removed: %s", hook.Phase, hook.Path))
			continue
		}
		if hook.SHA256 != currentHook.SHA256 {
			reasons = append(reasons, fmt.Sprintf("%s hook changed: %s", hook.Phase, hook.Path))
		}
	}
	for id, hook := range currentByID {
		if _, ok := savedByID[id]; !ok {
			reasons = append(reasons, fmt.Sprintf("%s hook added: %s", hook.Phase, hook.Path))
		}
	}

	sort.Strings(reasons)
	return reasons
}

func checkpointHookID(hook checkpointCompatibilityHook) string {
	return hook.Phase + ":" + hook.Path
}

func checkpointTypeMappingDiff(saved, current TypeMappingConfig) []string {
	var reasons []string
	appendIfChanged := func(name string, oldVal, newVal any) {
		if oldVal != newVal {
			reasons = append(reasons, fmt.Sprintf("type_mapping.%s changed: was %v, now %v", name, oldVal, newVal))
		}
	}

	appendIfChanged("tinyint1_as_boolean", saved.TinyInt1AsBoolean, current.TinyInt1AsBoolean)
	appendIfChanged("binary16_as_uuid", saved.Binary16AsUUID, current.Binary16AsUUID)
	appendIfChanged("datetime_as_timestamptz", saved.DatetimeAsTimestamptz, current.DatetimeAsTimestamptz)
	appendIfChanged("json_as_jsonb", saved.JSONAsJSONB, current.JSONAsJSONB)
	appendIfChanged("enum_mode", saved.EnumMode, current.EnumMode)
	appendIfChanged("set_mode", saved.SetMode, current.SetMode)
	appendIfChanged("widen_unsigned_integers", saved.WidenUnsignedIntegers, current.WidenUnsignedIntegers)
	appendIfChanged("varchar_as_text", saved.VarcharAsText, current.VarcharAsText)
	appendIfChanged("sanitize_json_null_bytes", saved.SanitizeJSONNullBytes, current.SanitizeJSONNullBytes)
	appendIfChanged("unknown_as_text", saved.UnknownAsText, current.UnknownAsText)
	appendIfChanged("collation_mode", saved.CollationMode, current.CollationMode)
	appendIfChanged("ci_as_citext", saved.CIAsCitext, current.CIAsCitext)
	appendIfChanged("bit_mode", saved.BitMode, current.BitMode)
	appendIfChanged("string_uuid_as_uuid", saved.StringUUIDAsUUID, current.StringUUIDAsUUID)
	appendIfChanged("binary16_uuid_mode", saved.Binary16UUIDMode, current.Binary16UUIDMode)
	appendIfChanged("time_mode", saved.TimeMode, current.TimeMode)
	appendIfChanged("zero_date_mode", saved.ZeroDateMode, current.ZeroDateMode)
	appendIfChanged("spatial_mode", saved.SpatialMode, current.SpatialMode)
	appendIfChanged("nvarchar_as_text", saved.NvarcharAsText, current.NvarcharAsText)
	appendIfChanged("money_as_numeric", saved.MoneyAsNumeric, current.MoneyAsNumeric)
	appendIfChanged("xml_as_text", saved.XmlAsText, current.XmlAsText)
	appendIfChanged("use_postgis", saved.UsePostGIS, current.UsePostGIS)

	reasons = append(reasons, checkpointCollationMapDiff(saved.CollationMap, current.CollationMap)...)
	sort.Strings(reasons)
	return reasons
}

func checkpointCollationMapDiff(saved, current map[string]string) []string {
	keys := make(map[string]struct{}, len(saved)+len(current))
	for key := range saved {
		keys[key] = struct{}{}
	}
	for key := range current {
		keys[key] = struct{}{}
	}

	var reasons []string
	for key := range keys {
		oldVal, oldOK := saved[key]
		newVal, newOK := current[key]
		switch {
		case !oldOK && newOK:
			reasons = append(reasons, fmt.Sprintf("type_mapping.collation_map[%q] added: %q", key, newVal))
		case oldOK && !newOK:
			reasons = append(reasons, fmt.Sprintf("type_mapping.collation_map[%q] removed (was %q)", key, oldVal))
		case oldVal != newVal:
			reasons = append(reasons, fmt.Sprintf("type_mapping.collation_map[%q] changed: was %q, now %q", key, oldVal, newVal))
		}
	}
	sort.Strings(reasons)
	return reasons
}

func checkpointTableCompatibilityDiff(saved, current []checkpointCompatibilityTable) []string {
	savedByName := make(map[string]checkpointCompatibilityTable, len(saved))
	currentByName := make(map[string]checkpointCompatibilityTable, len(current))

	for _, table := range saved {
		savedByName[table.SourceName] = table
	}
	for _, table := range current {
		currentByName[table.SourceName] = table
	}

	var reasons []string
	for name, table := range savedByName {
		currentTable, ok := currentByName[name]
		if !ok {
			reasons = append(reasons, fmt.Sprintf("table removed from migration: %s", name))
			continue
		}
		if table.PGName != currentTable.PGName {
			reasons = append(reasons, fmt.Sprintf("target table name changed for %s: was %q, now %q", name, table.PGName, currentTable.PGName))
		}
		if table.ChunkKey != currentTable.ChunkKey {
			reasons = append(reasons, fmt.Sprintf("chunk key changed for %s: was %q, now %q", name, table.ChunkKey, currentTable.ChunkKey))
		}
		if table.TableHash != currentTable.TableHash {
			reasons = append(reasons, fmt.Sprintf("table schema changed for %s", name))
		}
	}
	for name := range currentByName {
		if _, ok := savedByName[name]; !ok {
			reasons = append(reasons, fmt.Sprintf("table added to migration: %s", name))
		}
	}

	sort.Strings(reasons)
	return reasons
}
