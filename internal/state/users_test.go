package state

import (
	"context"
	"testing"
)

func TestUserDocuments(t *testing.T) {
	ctx := context.Background()
	type userStore interface {
		Store
		PutUser(context.Context, UserDocument) error
		ListUsers(context.Context) ([]UserDocument, error)
		DeleteUser(context.Context, string) error
	}
	sqlite, err := OpenSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]userStore{"memory": NewMemStore(), "sqlite": sqlite.(userStore)} {
		t.Run(name, func(t *testing.T) {
			defer func() { _ = s.Close() }()
			for _, u := range []UserDocument{{"bob", []byte(`{"v":1}`)}, {"alice", []byte(`{"v":1}`)}, {"bob", []byte(`{"v":2}`)}} {
				if err := s.PutUser(ctx, u); err != nil {
					t.Fatal(err)
				}
			}
			users, err := s.ListUsers(ctx)
			if err != nil || len(users) != 2 || users[0].Username != "alice" || string(users[1].Document) != `{"v":2}` {
				t.Errorf("ListUsers = %+v, %v; want alice, bob(v2)", users, err)
			}
			if err := s.DeleteUser(ctx, "bob"); err != nil {
				t.Fatal(err)
			}
			if err := s.DeleteUser(ctx, "nobody"); err != nil {
				t.Errorf("DeleteUser unknown = %v, want nil", err)
			}
			if users, _ := s.ListUsers(ctx); len(users) != 1 {
				t.Errorf("after delete: %+v", users)
			}
		})
	}
}

func TestUserDocumentsClosedDB(t *testing.T) {
	ctx := context.Background()
	st, err := OpenSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	s := st.(*sqlStore)
	_ = s.Close()
	if err := s.PutUser(ctx, UserDocument{Username: "x"}); err == nil {
		t.Error("PutUser on closed db: want error")
	}
	if _, err := s.ListUsers(ctx); err == nil {
		t.Error("ListUsers on closed db: want error")
	}
	if err := s.DeleteUser(ctx, "x"); err == nil {
		t.Error("DeleteUser on closed db: want error")
	}
}
