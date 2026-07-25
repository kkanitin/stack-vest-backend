package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
)

type GoogleUseCase struct {
	oauthCfg *oauth2.Config
	userRepo userdomain.Repository
}

func NewGoogleUseCase(clientID, clientSecret, redirectURL string, userRepo userdomain.Repository) *GoogleUseCase {
	return &GoogleUseCase{
		oauthCfg: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Scopes: []string{
				"https://www.googleapis.com/auth/userinfo.email",
				"https://www.googleapis.com/auth/userinfo.profile",
			},
			Endpoint: google.Endpoint,
		},
		userRepo: userRepo,
	}
}

func (uc *GoogleUseCase) GetAuthURL(state string) string {
	return uc.oauthCfg.AuthCodeURL(state, oauth2.AccessTypeOnline)
}

type googleUserInfo struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

func (uc *GoogleUseCase) HandleCallback(ctx context.Context, code string) (*userdomain.User, error) {
	token, err := uc.oauthCfg.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("code exchange failed: %w", err)
	}

	client := uc.oauthCfg.Client(ctx, token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}
	defer resp.Body.Close()

	info, err := decodeUserInfo(resp)
	if err != nil {
		return nil, err
	}

	return uc.userRepo.Upsert(ctx, &userdomain.User{
		GoogleID:  info.ID,
		Email:     info.Email,
		Name:      info.Name,
		Picture:   info.Picture,
		UpdatedAt: time.Now(),
	})
}

// decodeUserInfo validates the response status before decoding. client.Get only
// returns a non-nil err on a transport-level failure; a 4xx/5xx response (e.g.
// insufficient scope, rate-limited) comes back as err == nil with a JSON error
// envelope body, which would otherwise decode into a blank googleUserInfo{} and
// mask the real failure as a legitimate but empty user.
func decodeUserInfo(resp *http.Response) (*googleUserInfo, error) {
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("user info request failed: status %d: %s", resp.StatusCode, body)
	}
	var info googleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("failed to decode user info: %w", err)
	}
	return &info, nil
}
