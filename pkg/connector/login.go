// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"context"
	"fmt"

	"github.com/mattermost/mattermost/server/public/model"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

// GetLoginFlows returns the available login methods for the bridge.
func (mc *KChatConnector) GetLoginFlows() []bridgev2.LoginFlow {
	return []bridgev2.LoginFlow{
		{
			Name:        "Personal Access Token",
			Description: "Log in with a kChat personal access token",
			ID:          "token",
		},
		{
			Name:        "Password",
			Description: "Log in with username and password",
			ID:          "password",
		},
	}
}

// CreateLogin starts a new login process for the given flow.
func (mc *KChatConnector) CreateLogin(_ context.Context, user *bridgev2.User, flowID string) (bridgev2.LoginProcess, error) {
	switch flowID {
	case "token":
		return &TokenLoginProcess{
			connector: mc,
			user:      user,
		}, nil
	case "password":
		return &PasswordLoginProcess{
			connector: mc,
			user:      user,
		}, nil
	default:
		return nil, fmt.Errorf("unknown login flow: %s", flowID)
	}
}

// TokenLoginProcess implements token-based login.
type TokenLoginProcess struct {
	connector *KChatConnector
	user      *bridgev2.User
	serverURL string
}

var _ bridgev2.LoginProcessUserInput = (*TokenLoginProcess)(nil)

func (t *TokenLoginProcess) Start(_ context.Context) (*bridgev2.LoginStep, error) {
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeUserInput,
		StepID:       "io.github.rosch100.kchat.login.server_url",
		Instructions: "Enter your kChat server URL (e.g., https://your-team.kchat.infomaniak.com)",
		UserInputParams: &bridgev2.LoginUserInputParams{
			Fields: []bridgev2.LoginInputDataField{
				{
					Type: bridgev2.LoginInputFieldTypeURL,
					ID:   "server_url",
					Name: "Server URL",
				},
			},
		},
	}, nil
}

func (t *TokenLoginProcess) SubmitUserInput(ctx context.Context, input map[string]string) (*bridgev2.LoginStep, error) {
	if serverURL, ok := input["server_url"]; ok && t.serverURL == "" {
		t.serverURL = serverURL
		return &bridgev2.LoginStep{
			Type:         bridgev2.LoginStepTypeUserInput,
			StepID:       "io.github.rosch100.kchat.login.token",
			Instructions: "Enter your kChat personal access token",
			UserInputParams: &bridgev2.LoginUserInputParams{
				Fields: []bridgev2.LoginInputDataField{
					{
						Type: bridgev2.LoginInputFieldTypePassword,
						ID:   "token",
						Name: "Access Token",
					},
				},
			},
		}, nil
	}

	token := input["token"]
	return t.finishLogin(ctx, t.serverURL, token)
}

func (t *TokenLoginProcess) Cancel() {}

func (t *TokenLoginProcess) finishLogin(ctx context.Context, serverURL, token string) (*bridgev2.LoginStep, error) {
	result, err := validateTokenLogin(ctx, serverURL, token)
	if err != nil {
		return nil, err
	}

	loginID := MakeUserLoginID(result.User.Id)

	ul, err := t.user.NewLogin(ctx, &database.UserLogin{
		ID:         loginID,
		RemoteName: fmt.Sprintf("%s @ %s", result.User.Username, serverURL),
	}, &bridgev2.NewLoginParams{
		LoadUserLogin: t.connector.LoadUserLogin,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create login: %w", err)
	}

	meta := ul.Metadata.(*UserLoginMetadata)
	meta.ServerURL = serverURL
	meta.Token = token
	meta.UserID = result.User.Id
	meta.TeamID = result.TeamID
	if err := ul.Save(ctx); err != nil {
		return nil, fmt.Errorf("failed to save login: %w", err)
	}

	// Connect after saving.
	mmClient := ul.Client.(*KChatClient)
	mmClient.client = result.Client
	mmClient.serverURL = serverURL
	mmClient.userID = result.User.Id
	mmClient.teamID = result.TeamID
	mmClient.Connect(ctx)

	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeComplete,
		StepID:       "io.github.rosch100.kchat.login.complete",
		Instructions: fmt.Sprintf("Logged in as %s on %s", result.User.Username, serverURL),
		CompleteParams: &bridgev2.LoginCompleteParams{
			UserLoginID: loginID,
			UserLogin:   ul,
		},
	}, nil
}

// PasswordLoginProcess implements username/password login.
type PasswordLoginProcess struct {
	connector *KChatConnector
	user      *bridgev2.User
	serverURL string
}

var _ bridgev2.LoginProcessUserInput = (*PasswordLoginProcess)(nil)

func (p *PasswordLoginProcess) Start(_ context.Context) (*bridgev2.LoginStep, error) {
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeUserInput,
		StepID:       "io.github.rosch100.kchat.login.server_url",
		Instructions: "Enter your kChat server URL",
		UserInputParams: &bridgev2.LoginUserInputParams{
			Fields: []bridgev2.LoginInputDataField{
				{
					Type: bridgev2.LoginInputFieldTypeURL,
					ID:   "server_url",
					Name: "Server URL",
				},
			},
		},
	}, nil
}

func (p *PasswordLoginProcess) SubmitUserInput(ctx context.Context, input map[string]string) (*bridgev2.LoginStep, error) {
	if serverURL, ok := input["server_url"]; ok && p.serverURL == "" {
		p.serverURL = serverURL
		return &bridgev2.LoginStep{
			Type:         bridgev2.LoginStepTypeUserInput,
			StepID:       "io.github.rosch100.kchat.login.credentials",
			Instructions: "Enter your kChat credentials",
			UserInputParams: &bridgev2.LoginUserInputParams{
				Fields: []bridgev2.LoginInputDataField{
					{
						Type: bridgev2.LoginInputFieldTypeUsername,
						ID:   "username",
						Name: "Username",
					},
					{
						Type: bridgev2.LoginInputFieldTypePassword,
						ID:   "password",
						Name: "Password",
					},
				},
			},
		}, nil
	}

	username := input["username"]
	password := input["password"]

	client := model.NewAPIv4Client(p.serverURL)
	user, _, err := client.Login(ctx, username, password)
	if err != nil {
		return nil, fmt.Errorf("login failed: %w", err)
	}

	// Use the session token from the successful login.
	return p.finishLogin(ctx, p.serverURL, client.AuthToken, user)
}

func (p *PasswordLoginProcess) Cancel() {}

func (p *PasswordLoginProcess) finishLogin(ctx context.Context, serverURL, token string, me *model.User) (*bridgev2.LoginStep, error) {
	client := model.NewAPIv4Client(serverURL)
	client.SetToken(token)

	teamID, err := fetchFirstTeamID(ctx, client, me.Id)
	if err != nil {
		return nil, err
	}

	loginID := MakeUserLoginID(me.Id)

	ul, err := p.user.NewLogin(ctx, &database.UserLogin{
		ID:         loginID,
		RemoteName: fmt.Sprintf("%s @ %s", me.Username, serverURL),
	}, &bridgev2.NewLoginParams{
		LoadUserLogin: p.connector.LoadUserLogin,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create login: %w", err)
	}

	meta := ul.Metadata.(*UserLoginMetadata)
	meta.ServerURL = serverURL
	meta.Token = token
	meta.UserID = me.Id
	meta.TeamID = teamID
	if err := ul.Save(ctx); err != nil {
		return nil, fmt.Errorf("failed to save login: %w", err)
	}

	mmClient := ul.Client.(*KChatClient)
	mmClient.client = client
	mmClient.serverURL = serverURL
	mmClient.userID = me.Id
	mmClient.teamID = teamID
	mmClient.Connect(ctx)

	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeComplete,
		StepID:       "io.github.rosch100.kchat.login.complete",
		Instructions: fmt.Sprintf("Logged in as %s on %s", me.Username, serverURL),
		CompleteParams: &bridgev2.LoginCompleteParams{
			UserLoginID: loginID,
			UserLogin:   ul,
		},
	}, nil
}

// getLoginMeta is a helper to extract metadata from a UserLogin.
func getLoginMeta(login *bridgev2.UserLogin) *UserLoginMetadata {
	return login.Metadata.(*UserLoginMetadata)
}

// MakeGhostUserID creates a networkid.UserID for ghost creation.
func MakeGhostUserID(mmUserID string) networkid.UserID {
	return networkid.UserID(mmUserID)
}

// loginResult holds the validated result of a token login attempt.
type loginResult struct {
	User   *model.User
	TeamID string
	Client *model.Client4
}

// validateTokenLogin authenticates with the given serverURL and token,
// retrieves the user profile and teams. Returns the validated result or an error.
func validateTokenLogin(ctx context.Context, serverURL, token string) (*loginResult, error) {
	client := model.NewAPIv4Client(serverURL)
	client.SetToken(token)

	me, _, err := client.GetMe(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	teamID, err := fetchFirstTeamID(ctx, client, me.Id)
	if err != nil {
		return nil, err
	}

	return &loginResult{
		User:   me,
		TeamID: teamID,
		Client: client,
	}, nil
}

// fetchFirstTeamID fetches teams for a user and returns the first team's ID,
// or empty string if the user has no teams.
func fetchFirstTeamID(ctx context.Context, client *model.Client4, userID string) (string, error) {
	teams, _, err := client.GetTeamsForUser(ctx, userID, "")
	if err != nil {
		return "", fmt.Errorf("failed to get teams: %w", err)
	}
	if len(teams) > 0 {
		return teams[0].Id, nil
	}
	return "", nil
}
