package api

import (
	"DataArk/auth"
	"DataArk/discovery"
	"errors"
	"net/http"
	"testing"
)

func TestDiscoveryDomainBlacklistRequiresOwner(t *testing.T) {
	oldList := listDiscoveryDomainBlacklist
	oldCreate := createDiscoveryDomainBlacklist
	oldDelete := deleteDiscoveryDomainBlacklist
	t.Cleanup(func() {
		listDiscoveryDomainBlacklist = oldList
		createDiscoveryDomainBlacklist = oldCreate
		deleteDiscoveryDomainBlacklist = oldDelete
	})
	listCalls, createCalls, deleteCalls := 0, 0, 0
	listDiscoveryDomainBlacklist = func() ([]discovery.DiscoveryDomainBlacklistEntry, error) {
		listCalls++
		return []discovery.DiscoveryDomainBlacklistEntry{{ID: 1, Domain: "csdn.net"}}, nil
	}
	createDiscoveryDomainBlacklist = func(domain string, reason string) (*discovery.DiscoveryDomainBlacklistMutation, error) {
		createCalls++
		return &discovery.DiscoveryDomainBlacklistMutation{Entry: discovery.DiscoveryDomainBlacklistEntry{ID: 2, Domain: domain, Reason: reason}, AffectedCandidates: 3}, nil
	}
	deleteDiscoveryDomainBlacklist = func(id uint) (*discovery.DiscoveryDomainBlacklistMutation, error) {
		deleteCalls++
		return &discovery.DiscoveryDomainBlacklistMutation{Entry: discovery.DiscoveryDomainBlacklistEntry{ID: id, Domain: "csdn.net"}, AffectedCandidates: 2}, nil
	}
	member := &auth.User{ID: 2, Role: auth.UserRoleMember}
	owner := &auth.User{ID: 1, Role: auth.UserRoleOwner}
	if response := performUserControllerRequest(http.MethodGet, "/admin/discovery/domain-blacklist", nil, member, ListDiscoveryDomainBlacklist); response.Code != http.StatusForbidden {
		t.Fatalf("member list status = %d", response.Code)
	}
	if response := performUserControllerRequest(http.MethodPost, "/admin/discovery/domain-blacklist", []byte(`{"domain":"example.com"}`), member, CreateDiscoveryDomainBlacklist); response.Code != http.StatusForbidden {
		t.Fatalf("member create status = %d", response.Code)
	}
	if response := performUserPathControllerRequest(http.MethodDelete, "/admin/discovery/domain-blacklist/:id", "/admin/discovery/domain-blacklist/1", member, DeleteDiscoveryDomainBlacklist); response.Code != http.StatusForbidden {
		t.Fatalf("member delete status = %d", response.Code)
	}
	if listCalls != 0 || createCalls != 0 || deleteCalls != 0 {
		t.Fatalf("member calls = %d/%d/%d", listCalls, createCalls, deleteCalls)
	}

	listResponse := performUserControllerRequest(http.MethodGet, "/admin/discovery/domain-blacklist", nil, owner, ListDiscoveryDomainBlacklist)
	if listResponse.Code != http.StatusOK || listCalls != 1 {
		t.Fatalf("owner list status=%d calls=%d", listResponse.Code, listCalls)
	}
	createResponse := performUserControllerRequest(http.MethodPost, "/admin/discovery/domain-blacklist", []byte(`{"domain":"blog.example.com","reason":"fixture"}`), owner, CreateDiscoveryDomainBlacklist)
	if createResponse.Code != http.StatusCreated || createCalls != 1 {
		t.Fatalf("owner create status=%d calls=%d", createResponse.Code, createCalls)
	}
	data := decodeResponse(t, createResponse)["Data"].(map[string]interface{})
	if data["affectedCandidates"] != float64(3) {
		t.Fatalf("create data = %#v", data)
	}
	deleteResponse := performUserPathControllerRequest(http.MethodDelete, "/admin/discovery/domain-blacklist/:id", "/admin/discovery/domain-blacklist/2", owner, DeleteDiscoveryDomainBlacklist)
	if deleteResponse.Code != http.StatusOK || deleteCalls != 1 {
		t.Fatalf("owner delete status=%d calls=%d", deleteResponse.Code, deleteCalls)
	}
}

func TestCreateDiscoveryDomainBlacklistMapsValidationErrors(t *testing.T) {
	oldCreate := createDiscoveryDomainBlacklist
	t.Cleanup(func() { createDiscoveryDomainBlacklist = oldCreate })
	owner := &auth.User{ID: 1, Role: auth.UserRoleOwner}
	createDiscoveryDomainBlacklist = func(string, string) (*discovery.DiscoveryDomainBlacklistMutation, error) {
		return nil, discovery.ErrInvalidDiscoveryBlacklistDomain
	}
	invalid := performUserControllerRequest(http.MethodPost, "/admin/discovery/domain-blacklist", []byte(`{"domain":"bad"}`), owner, CreateDiscoveryDomainBlacklist)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d", invalid.Code)
	}
	createDiscoveryDomainBlacklist = func(string, string) (*discovery.DiscoveryDomainBlacklistMutation, error) {
		return nil, discovery.ErrDuplicateDiscoveryBlacklistDomain
	}
	duplicate := performUserControllerRequest(http.MethodPost, "/admin/discovery/domain-blacklist", []byte(`{"domain":"csdn.net"}`), owner, CreateDiscoveryDomainBlacklist)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d", duplicate.Code)
	}
	createDiscoveryDomainBlacklist = func(string, string) (*discovery.DiscoveryDomainBlacklistMutation, error) {
		return nil, errors.New("database unavailable")
	}
	failure := performUserControllerRequest(http.MethodPost, "/admin/discovery/domain-blacklist", []byte(`{"domain":"example.com"}`), owner, CreateDiscoveryDomainBlacklist)
	if failure.Code != http.StatusInternalServerError {
		t.Fatalf("failure status = %d", failure.Code)
	}
}
