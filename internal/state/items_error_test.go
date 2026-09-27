package state

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

const (
	deleteItemsSQL = `DELETE FROM config_items WHERE kind = ?`
	upsertItemsSQL = `INSERT INTO config_items (kind, name, value) VALUES (?, ?, ?) ON CONFLICT (kind, name) DO UPDATE SET value = excluded.value`
)

func TestSQLStoreDeleteItemErrors(t *testing.T) {
	t.Run("exec", func(t *testing.T) {
		s, mock := newMockSQLStore(t)
		wantErr := errors.New("delete boom")
		mock.ExpectExec(`DELETE FROM config_items WHERE kind = ? AND name = ?`).
			WithArgs("map", "region").
			WillReturnError(wantErr)

		if err := s.DeleteItem(context.Background(), "map", "region"); !errors.Is(err, wantErr) {
			t.Fatalf("DeleteItem error = %v, want %v", err, wantErr)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("rows affected", func(t *testing.T) {
		s, mock := newMockSQLStore(t)
		wantErr := errors.New("result boom")
		mock.ExpectExec(`DELETE FROM config_items WHERE kind = ? AND name = ?`).
			WithArgs("map", "region").
			WillReturnResult(sqlmock.NewErrorResult(wantErr))

		if err := s.DeleteItem(context.Background(), "map", "region"); !errors.Is(err, wantErr) {
			t.Fatalf("DeleteItem error = %v, want %v", err, wantErr)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})
}

func TestSQLStoreReplaceItemsErrors(t *testing.T) {
	items := map[string]json.RawMessage{"region": json.RawMessage(`"eu"`)}
	tests := []struct {
		name  string
		setup func(sqlmock.Sqlmock, error)
	}{
		{
			name: "begin",
			setup: func(mock sqlmock.Sqlmock, wantErr error) {
				mock.ExpectBegin().WillReturnError(wantErr)
			},
		},
		{
			name: "delete",
			setup: func(mock sqlmock.Sqlmock, wantErr error) {
				mock.ExpectBegin()
				mock.ExpectExec(deleteItemsSQL).WithArgs("map").WillReturnError(wantErr)
				mock.ExpectRollback()
			},
		},
		{
			name: "prepare",
			setup: func(mock sqlmock.Sqlmock, wantErr error) {
				mock.ExpectBegin()
				mock.ExpectExec(deleteItemsSQL).WithArgs("map").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectPrepare(upsertItemsSQL).WillReturnError(wantErr)
				mock.ExpectRollback()
			},
		},
		{
			name: "exec",
			setup: func(mock sqlmock.Sqlmock, wantErr error) {
				mock.ExpectBegin()
				mock.ExpectExec(deleteItemsSQL).WithArgs("map").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectPrepare(upsertItemsSQL).
					ExpectExec().
					WithArgs("map", "region", `"eu"`).
					WillReturnError(wantErr)
				mock.ExpectRollback()
			},
		},
		{
			name: "commit",
			setup: func(mock sqlmock.Sqlmock, wantErr error) {
				mock.ExpectBegin()
				mock.ExpectExec(deleteItemsSQL).WithArgs("map").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectPrepare(upsertItemsSQL).
					ExpectExec().
					WithArgs("map", "region", `"eu"`).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit().WillReturnError(wantErr)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, mock := newMockSQLStore(t)
			wantErr := errors.New(tt.name + " boom")
			tt.setup(mock, wantErr)

			if err := s.ReplaceItems(context.Background(), "map", items); !errors.Is(err, wantErr) {
				t.Fatalf("ReplaceItems error = %v, want %v", err, wantErr)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unmet expectations: %v", err)
			}
		})
	}
}

func TestSQLStorePutItemsErrors(t *testing.T) {
	items := map[string]json.RawMessage{"region": json.RawMessage(`"eu"`)}
	tests := []struct {
		name  string
		setup func(sqlmock.Sqlmock, error)
	}{
		{
			name: "begin",
			setup: func(mock sqlmock.Sqlmock, wantErr error) {
				mock.ExpectBegin().WillReturnError(wantErr)
			},
		},
		{
			name: "prepare",
			setup: func(mock sqlmock.Sqlmock, wantErr error) {
				mock.ExpectBegin()
				mock.ExpectPrepare(upsertItemsSQL).WillReturnError(wantErr)
				mock.ExpectRollback()
			},
		},
		{
			name: "exec",
			setup: func(mock sqlmock.Sqlmock, wantErr error) {
				mock.ExpectBegin()
				mock.ExpectPrepare(upsertItemsSQL).
					ExpectExec().
					WithArgs("map", "region", `"eu"`).
					WillReturnError(wantErr)
				mock.ExpectRollback()
			},
		},
		{
			name: "commit",
			setup: func(mock sqlmock.Sqlmock, wantErr error) {
				mock.ExpectBegin()
				mock.ExpectPrepare(upsertItemsSQL).
					ExpectExec().
					WithArgs("map", "region", `"eu"`).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit().WillReturnError(wantErr)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, mock := newMockSQLStore(t)
			wantErr := errors.New(tt.name + " boom")
			tt.setup(mock, wantErr)

			if err := s.PutItems(context.Background(), "map", items); !errors.Is(err, wantErr) {
				t.Fatalf("PutItems error = %v, want %v", err, wantErr)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unmet expectations: %v", err)
			}
		})
	}
}
