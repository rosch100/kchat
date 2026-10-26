// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"context"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

func TestGetLoginFlows(t *testing.T) {
	mc := &KChatConnector{}
	flows := mc.GetLoginFlows()

	if len(flows) != 1 {
		t.Fatalf("GetLoginFlows: got %d flows, want 1", len(flows))
	}

	if flows[0].ID != "token" {
		t.Errorf("flows[0].ID: got %q, want %q", flows[0].ID, "token")
	}

	for i, flow := range flows {
		if flow.Name == "" {
			t.Errorf("flows[%d].Name should not be empty", i)
		}
		if flow.Description == "" {
			t.Errorf("flows[%d].Description should not be empty", i)
		}
	}
}

func TestCreateLogin_Token(t *testing.T) {
	mc := &KChatConnector{}
	ctx := context.Background()

	proc, err := mc.CreateLogin(ctx, nil, "token")
	if err != nil {
		t.Fatalf("CreateLogin(token): unexpected error: %v", err)
	}

	tp, ok := proc.(*TokenLoginProcess)
	if !ok {
		t.Fatalf("CreateLogin(token): got %T, want *TokenLoginProcess", proc)
	}
	if tp.connector != mc {
		t.Error("TokenLoginProcess.connector should be the connector")
	}
}

func TestCreateLogin_UnknownFlow(t *testing.T) {
	mc := &KChatConnector{}
	ctx := context.Background()

	proc, err := mc.CreateLogin(ctx, nil, "sso")
	if err == nil {
		t.Fatal("CreateLogin(sso): expected error, got nil")
	}
	if proc != nil {
		t.Errorf("CreateLogin(sso): expected nil process, got %T", proc)
	}
}

func TestTokenLoginStart(t *testing.T) {
	mc := &KChatConnector{}
	tp := &TokenLoginProcess{connector: mc}
	ctx := context.Background()

	step, err := tp.Start(ctx)
	if err != nil {
		t.Fatalf("TokenLoginProcess.Start: unexpected error: %v", err)
	}

	if step.Type != bridgev2.LoginStepTypeUserInput {
		t.Errorf("step.Type: got %q, want %q", step.Type, bridgev2.LoginStepTypeUserInput)
	}
	if step.StepID != "io.github.rosch100.kchat.login.server_url" {
		t.Errorf("step.StepID: got %q, want %q", step.StepID, "io.github.rosch100.kchat.login.server_url")
	}
	if step.UserInputParams == nil {
		t.Fatal("step.UserInputParams should not be nil")
	}
	if len(step.UserInputParams.Fields) != 1 {
		t.Fatalf("fields count: got %d, want 1", len(step.UserInputParams.Fields))
	}
	if step.UserInputParams.Fields[0].ID != "server_url" {
		t.Errorf("field[0].ID: got %q, want %q", step.UserInputParams.Fields[0].ID, "server_url")
	}
}

func TestTokenSubmitUserInput_ServerURL(t *testing.T) {
	mc := &KChatConnector{}
	tp := &TokenLoginProcess{connector: mc}
	ctx := context.Background()

	step, err := tp.SubmitUserInput(ctx, map[string]string{
		"server_url": "https://mm.example.com",
	})
	if err != nil {
		t.Fatalf("SubmitUserInput(server_url): unexpected error: %v", err)
	}

	if tp.serverURL != "https://mm.example.com" {
		t.Errorf("serverURL: got %q, want %q", tp.serverURL, "https://mm.example.com")
	}
	if step.StepID != "io.github.rosch100.kchat.login.token" {
		t.Errorf("step.StepID: got %q, want %q", step.StepID, "io.github.rosch100.kchat.login.token")
	}
	if step.Type != bridgev2.LoginStepTypeUserInput {
		t.Errorf("step.Type: got %q, want %q", step.Type, bridgev2.LoginStepTypeUserInput)
	}
	if step.UserInputParams == nil {
		t.Fatal("step.UserInputParams should not be nil")
	}
	if len(step.UserInputParams.Fields) != 1 {
		t.Fatalf("fields count: got %d, want 1", len(step.UserInputParams.Fields))
	}
	if step.UserInputParams.Fields[0].ID != "token" {
		t.Errorf("field[0].ID: got %q, want %q", step.UserInputParams.Fields[0].ID, "token")
	}
}

func TestTokenCancel(t *testing.T) {
	tp := &TokenLoginProcess{}
	// Cancel should not panic.
	tp.Cancel()
}

func TestMakeGhostUserID(t *testing.T) {
	got := MakeGhostUserID("user123")
	want := networkid.UserID("user123")
	if got != want {
		t.Errorf("MakeGhostUserID: got %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// validateTokenLogin tests
// ---------------------------------------------------------------------------

func TestValidateTokenLogin_Success(t *testing.T) {
	fake := newFakeMM()
	defer fake.Close()

	fake.Users["uid1"] = &model.User{Id: "uid1", Username: "testuser"}
	fake.TokenToUser["test-tok-123"] = "uid1"
	fake.Teams["uid1"] = []*model.Team{{Id: "team1", Name: "My Team"}}

	ctx := context.Background()
	result, err := validateTokenLogin(ctx, fake.Server.URL, "test-tok-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.User.Id != "uid1" {
		t.Errorf("user ID: got %q, want %q", result.User.Id, "uid1")
	}
	if result.User.Username != "testuser" {
		t.Errorf("username: got %q, want %q", result.User.Username, "testuser")
	}
	if result.TeamID != "team1" {
		t.Errorf("team ID: got %q, want %q", result.TeamID, "team1")
	}
	if result.Client == nil {
		t.Error("expected non-nil client")
	}

	// Verify the fake server was called for authentication and team retrieval.
	if !fake.CalledPath("/api/v4/users/me") {
		t.Error("expected /api/v4/users/me to be called")
	}
	if !fake.CalledPath("/teams") {
		t.Error("expected /users/{id}/teams to be called")
	}
}

func TestValidateTokenLogin_AuthFailed(t *testing.T) {
	fake := newFakeMM()
	defer fake.Close()
	// No users/tokens registered — GetMe returns 401.

	ctx := context.Background()
	result, err := validateTokenLogin(ctx, fake.Server.URL, "bad-token")
	if err == nil {
		t.Fatal("expected authentication failure error, got nil")
	}
	if result != nil {
		t.Errorf("expected nil result on error, got %+v", result)
	}
}

func TestValidateTokenLogin_NoTeams(t *testing.T) {
	fake := newFakeMM()
	defer fake.Close()

	fake.Users["uid4"] = &model.User{Id: "uid4", Username: "dave"}
	fake.TokenToUser["tok-4"] = "uid4"
	// No teams registered — empty teams list.

	ctx := context.Background()
	result, err := validateTokenLogin(ctx, fake.Server.URL, "tok-4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.TeamID != "" {
		t.Errorf("team ID: got %q, want empty", result.TeamID)
	}
	if result.User.Id != "uid4" {
		t.Errorf("user ID: got %q, want %q", result.User.Id, "uid4")
	}
}

// ---------------------------------------------------------------------------
// fetchFirstTeamID tests
// ---------------------------------------------------------------------------

func TestFetchFirstTeamID_Success(t *testing.T) {
	fake := newFakeMM()
	defer fake.Close()

	fake.Users["uid3"] = &model.User{Id: "uid3", Username: "charlie"}
	fake.TokenToUser["pass-tok-2"] = "uid3"
	fake.Teams["uid3"] = []*model.Team{{Id: "team-x", Name: "Team X"}}

	client := model.NewAPIv4Client(fake.Server.URL)
	client.SetToken("pass-tok-2")

	ctx := context.Background()
	teamID, err := fetchFirstTeamID(ctx, client, "uid3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if teamID != "team-x" {
		t.Errorf("team ID: got %q, want %q", teamID, "team-x")
	}

	// Verify teams were fetched.
	if !fake.CalledPath("/teams") {
		t.Error("expected /teams to be called")
	}
}

func TestFetchFirstTeamID_Error(t *testing.T) {
	fake := newFakeMM()
	defer fake.Close()

	fake.FailEndpoints["/teams"] = true

	client := model.NewAPIv4Client(fake.Server.URL)
	client.SetToken("some-tok")

	ctx := context.Background()
	_, err := fetchFirstTeamID(ctx, client, "uid2")
	if err == nil {
		t.Fatal("expected teams failure error, got nil")
	}
}

func TestFetchFirstTeamID_NoTeams(t *testing.T) {
	fake := newFakeMM()
	defer fake.Close()

	// No teams registered for this user.
	client := model.NewAPIv4Client(fake.Server.URL)
	client.SetToken("some-tok")

	ctx := context.Background()
	teamID, err := fetchFirstTeamID(ctx, client, "uid-no-teams")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if teamID != "" {
		t.Errorf("team ID: got %q, want empty", teamID)
	}
}

func TestFetchFirstTeamID_MultipleTeams(t *testing.T) {
	fake := newFakeMM()
	defer fake.Close()

	fake.Users["uid5"] = &model.User{Id: "uid5", Username: "eve"}
	fake.TokenToUser["tok-5"] = "uid5"
	fake.Teams["uid5"] = []*model.Team{
		{Id: "team-first", Name: "First Team"},
		{Id: "team-second", Name: "Second Team"},
		{Id: "team-third", Name: "Third Team"},
	}

	client := model.NewAPIv4Client(fake.Server.URL)
	client.SetToken("tok-5")

	ctx := context.Background()
	teamID, err := fetchFirstTeamID(ctx, client, "uid5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return the FIRST team's ID, not any other.
	if teamID != "team-first" {
		t.Errorf("team ID: got %q, want %q (should be first team)", teamID, "team-first")
	}
}

func TestValidateTokenLogin_ClientTokenSet(t *testing.T) {
	fake := newFakeMM()
	defer fake.Close()

	fake.Users["uid6"] = &model.User{Id: "uid6", Username: "frank"}
	fake.TokenToUser["tok-6"] = "uid6"
	fake.Teams["uid6"] = []*model.Team{{Id: "team1", Name: "T1"}}

	ctx := context.Background()
	result, err := validateTokenLogin(ctx, fake.Server.URL, "tok-6")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the returned client has the token set.
	if result.Client == nil {
		t.Fatal("expected non-nil client")
	}
	if result.Client.AuthToken != "tok-6" {
		t.Errorf("client AuthToken: got %q, want %q", result.Client.AuthToken, "tok-6")
	}
}

// ---------------------------------------------------------------------------
// Password login integration tests
// ---------------------------------------------------------------------------

func TestGetLoginMeta(t *testing.T) {
	meta := &UserLoginMetadata{
		ServerURL: "http://mm.test:8065",
		Token:     "tok123",
		UserID:    "uid1",
		TeamID:    "team1",
	}
	dbLogin := &database.UserLogin{
		Metadata: meta,
	}
	login := &bridgev2.UserLogin{
		UserLogin: dbLogin,
	}

	got := getLoginMeta(login)
	if got != meta {
		t.Error("getLoginMeta should return the same metadata pointer")
	}
	if got.ServerURL != "http://mm.test:8065" {
		t.Errorf("ServerURL: got %q", got.ServerURL)
	}
}
