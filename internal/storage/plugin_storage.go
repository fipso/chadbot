package storage

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	pb "github.com/fipso/chadbot/gen/chadbot"
)

// ValidTableNameRegex ensures table names are safe
var ValidTableNameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)

// PluginStorage handles namespaced storage for plugins
type PluginStorage struct {
	pluginID string
	prefix   string
}

// NewPluginStorage creates a new plugin storage handler
func NewPluginStorage(pluginID string) *PluginStorage {
	// Create a safe prefix from plugin ID (first 8 chars of UUID)
	prefix := "plugin_" + strings.ReplaceAll(pluginID[:8], "-", "")
	return &PluginStorage{
		pluginID: pluginID,
		prefix:   prefix,
	}
}

// NamespacedTable returns the full table name with plugin prefix
func (ps *PluginStorage) NamespacedTable(tableName string) (string, error) {
	if !ValidTableNameRegex.MatchString(tableName) {
		return "", fmt.Errorf("invalid table name: %s", tableName)
	}
	return ps.prefix + "_" + tableName, nil
}

// HandleRequest processes a storage request from a plugin
func (ps *PluginStorage) HandleRequest(req *pb.StorageRequest) *pb.StorageResponse {
	resp := &pb.StorageResponse{
		RequestId: req.RequestId,
	}

	switch op := req.Operation.(type) {
	case *pb.StorageRequest_CreateTable:
		resp = ps.createTable(req.RequestId, op.CreateTable)
	case *pb.StorageRequest_DropTable:
		resp = ps.dropTable(req.RequestId, op.DropTable)
	case *pb.StorageRequest_Insert:
		resp = ps.insert(req.RequestId, op.Insert)
	case *pb.StorageRequest_Update:
		resp = ps.update(req.RequestId, op.Update)
	case *pb.StorageRequest_Delete:
		resp = ps.delete(req.RequestId, op.Delete)
	case *pb.StorageRequest_Query:
		resp = ps.query(req.RequestId, op.Query)
	default:
		resp.Success = false
		resp.Error = "unknown operation"
	}

	return resp
}

func (ps *PluginStorage) createTable(reqID string, req *pb.CreateTableRequest) *pb.StorageResponse {
	resp := &pb.StorageResponse{RequestId: reqID}

	tableName, err := ps.NamespacedTable(req.TableName)
	if err != nil {
		resp.Error = err.Error()
		return resp
	}

	// Build CREATE TABLE statement
	var cols []string
	for _, col := range req.Columns {
		colDef := fmt.Sprintf("%s %s", col.Name, col.Type)
		if col.PrimaryKey {
			colDef += " PRIMARY KEY"
		}
		if col.NotNull {
			colDef += " NOT NULL"
		}
		if col.Unique {
			colDef += " UNIQUE"
		}
		if col.DefaultValue != "" {
			colDef += fmt.Sprintf(" DEFAULT %s", col.DefaultValue)
		}
		cols = append(cols, colDef)
	}

	ifNotExists := ""
	if req.IfNotExists {
		ifNotExists = "IF NOT EXISTS "
	}

	query := fmt.Sprintf("CREATE TABLE %s%s (%s)", ifNotExists, tableName, strings.Join(cols, ", "))

	if err := DB.Exec(query).Error; err != nil {
		resp.Error = err.Error()
		return resp
	}

	resp.Success = true
	return resp
}

func (ps *PluginStorage) dropTable(reqID string, req *pb.DropTableRequest) *pb.StorageResponse {
	resp := &pb.StorageResponse{RequestId: reqID}

	tableName, err := ps.NamespacedTable(req.TableName)
	if err != nil {
		resp.Error = err.Error()
		return resp
	}

	ifExists := ""
	if req.IfExists {
		ifExists = "IF EXISTS "
	}

	query := fmt.Sprintf("DROP TABLE %s%s", ifExists, tableName)

	if err := DB.Exec(query).Error; err != nil {
		resp.Error = err.Error()
		return resp
	}

	resp.Success = true
	return resp
}

func (ps *PluginStorage) insert(reqID string, req *pb.InsertRequest) *pb.StorageResponse {
	resp := &pb.StorageResponse{RequestId: reqID}

	tableName, err := ps.NamespacedTable(req.TableName)
	if err != nil {
		resp.Error = err.Error()
		return resp
	}

	var columns []string
	var placeholders []string
	var values []interface{}

	for col, val := range req.Values {
		columns = append(columns, col)
		placeholders = append(placeholders, "?")
		values = append(values, val)
	}

	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		tableName,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "))

	result := DB.Exec(query, values...)
	if result.Error != nil {
		resp.Error = result.Error.Error()
		return resp
	}

	resp.Success = true
	resp.AffectedRows = result.RowsAffected

	// Get last insert ID using raw SQL
	var lastID int64
	DB.Raw("SELECT last_insert_rowid()").Scan(&lastID)
	resp.LastInsertId = lastID

	return resp
}

func (ps *PluginStorage) update(reqID string, req *pb.UpdateRequest) *pb.StorageResponse {
	resp := &pb.StorageResponse{RequestId: reqID}

	tableName, err := ps.NamespacedTable(req.TableName)
	if err != nil {
		resp.Error = err.Error()
		return resp
	}

	var setClauses []string
	var values []interface{}

	for col, val := range req.Values {
		setClauses = append(setClauses, fmt.Sprintf("%s = ?", col))
		values = append(values, val)
	}

	query := fmt.Sprintf("UPDATE %s SET %s", tableName, strings.Join(setClauses, ", "))

	if req.WhereClause != "" {
		query += " WHERE " + req.WhereClause
		for _, arg := range req.WhereArgs {
			values = append(values, arg)
		}
	}

	result := DB.Exec(query, values...)
	if result.Error != nil {
		resp.Error = result.Error.Error()
		return resp
	}

	resp.Success = true
	resp.AffectedRows = result.RowsAffected
	return resp
}

func (ps *PluginStorage) delete(reqID string, req *pb.DeleteRequest) *pb.StorageResponse {
	resp := &pb.StorageResponse{RequestId: reqID}

	tableName, err := ps.NamespacedTable(req.TableName)
	if err != nil {
		resp.Error = err.Error()
		return resp
	}

	query := fmt.Sprintf("DELETE FROM %s", tableName)
	var values []interface{}

	if req.WhereClause != "" {
		query += " WHERE " + req.WhereClause
		for _, arg := range req.WhereArgs {
			values = append(values, arg)
		}
	}

	result := DB.Exec(query, values...)
	if result.Error != nil {
		resp.Error = result.Error.Error()
		return resp
	}

	resp.Success = true
	resp.AffectedRows = result.RowsAffected
	return resp
}

func (ps *PluginStorage) query(reqID string, req *pb.QueryRequest) *pb.StorageResponse {
	resp := &pb.StorageResponse{RequestId: reqID}

	tableName, err := ps.NamespacedTable(req.TableName)
	if err != nil {
		resp.Error = err.Error()
		return resp
	}

	columns := "*"
	if len(req.Columns) > 0 {
		columns = strings.Join(req.Columns, ", ")
	}

	query := fmt.Sprintf("SELECT %s FROM %s", columns, tableName)
	var values []interface{}

	if req.WhereClause != "" {
		query += " WHERE " + req.WhereClause
		for _, arg := range req.WhereArgs {
			values = append(values, arg)
		}
	}

	if req.OrderBy != "" {
		query += " ORDER BY " + req.OrderBy
	}

	if req.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", req.Limit)
	}

	if req.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", req.Offset)
	}

	sqlDB, err := DB.DB()
	if err != nil {
		resp.Error = err.Error()
		return resp
	}

	rows, err := sqlDB.Query(query, values...)
	if err != nil {
		resp.Error = err.Error()
		return resp
	}
	defer rows.Close()

	colNames, err := rows.Columns()
	if err != nil {
		resp.Error = err.Error()
		return resp
	}

	for rows.Next() {
		// Create slice to hold column values
		columnPtrs := make([]interface{}, len(colNames))
		columnVals := make([]sql.NullString, len(colNames))
		for i := range columnPtrs {
			columnPtrs[i] = &columnVals[i]
		}

		if err := rows.Scan(columnPtrs...); err != nil {
			resp.Error = err.Error()
			return resp
		}

		row := &pb.Row{
			Values: make(map[string]string),
		}
		for i, colName := range colNames {
			if columnVals[i].Valid {
				row.Values[colName] = columnVals[i].String
			} else {
				row.Values[colName] = ""
			}
		}
		resp.Rows = append(resp.Rows, row)
	}

	resp.Success = true
	return resp
}
