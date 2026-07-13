package auth

import "testing"

func TestHashPasswordAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("secret-password")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if hash == "" || hash == "secret-password" {
		t.Fatalf("unexpected hash: %q", hash)
	}
	if !checkPassword("secret-password", hash) {
		t.Fatal("checkPassword should accept the original password")
	}
	if checkPassword("wrong-password", hash) {
		t.Fatal("checkPassword should reject a wrong password")
	}
}

func TestUserDatabaseOperations(t *testing.T) {
	setupSQLiteDB(t)

	user, err := CreateUser("bob", "secret123")
	if err != nil {
		t.Fatalf("CreateUser returned error: %v", err)
	}
	if user.ID == 0 || user.Password == "secret123" {
		t.Fatalf("unexpected user: %#v", user)
	}
	if user.Role != UserRoleMember {
		t.Fatalf("member role = %q", user.Role)
	}
	if _, err := CreateUser("bob", "secret123"); err == nil {
		t.Fatal("duplicate user should return error")
	}

	admin, err := CreateUser("admin", "secret123")
	if err != nil || admin == nil {
		t.Fatalf("initial admin create = %#v err=%v", admin, err)
	}
	if admin.Role != UserRoleOwner {
		t.Fatalf("admin role = %q", admin.Role)
	}
	admin, err = CreateUser("admin", "secret123")
	if err != nil || admin != nil {
		t.Fatalf("duplicate admin create = %#v err=%v, want nil nil", admin, err)
	}

	loggedIn, err := LoginUser("bob", "secret123")
	if err != nil {
		t.Fatalf("LoginUser returned error: %v", err)
	}
	if loggedIn.ID != user.ID {
		t.Fatalf("loggedIn ID = %d, want %d", loggedIn.ID, user.ID)
	}
	if _, err := LoginUser("bob", "wrong"); err == nil {
		t.Fatal("wrong password should fail")
	}
	if _, err := LoginUser("missing", "secret123"); err == nil {
		t.Fatal("missing user should fail")
	}

	byID, err := GetUserByID(user.ID)
	if err != nil || byID.Username != "bob" {
		t.Fatalf("GetUserByID = %#v err=%v", byID, err)
	}
	byUsername, err := GetUserByUsername("bob")
	if err != nil || byUsername.ID != user.ID {
		t.Fatalf("GetUserByUsername = %#v err=%v", byUsername, err)
	}

	updated, err := UpdateUser(user.ID, map[string]interface{}{
		"username": "bobby",
		"password": "newsecret",
	})
	if err != nil {
		t.Fatalf("UpdateUser returned error: %v", err)
	}
	if updated.Username != "bobby" || !checkPassword("newsecret", updated.Password) {
		t.Fatalf("unexpected updated user: %#v", updated)
	}
	if _, err := UpdateUser(9999, map[string]interface{}{"username": "none"}); err == nil {
		t.Fatal("updating missing user should fail")
	}

	users, total, err := GetAllUsers(1, 10)
	if err != nil {
		t.Fatalf("GetAllUsers returned error: %v", err)
	}
	if total != 2 || len(users) != 2 {
		t.Fatalf("users=%#v total=%d, want 2", users, total)
	}

	if err := DeleteUser(user.ID); err != nil {
		t.Fatalf("DeleteUser returned error: %v", err)
	}
	if _, err := GetUserByID(user.ID); err == nil {
		t.Fatal("deleted user should not be found")
	}
}
