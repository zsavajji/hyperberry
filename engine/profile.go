package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/appstate"
	"github.com/polymorfa/hypermeow/types"
)

// My profile (step 5.9): profile.get and profile.set, for the real and the simulated account.
// The picture is a JPEG file, data/profile/me.jpg; the engine never decodes it (phase6.md 6.3),
// the UI makes the 640x640 JPEG it sends.

const (
	maxProfileName  = 25      // WhatsApp's own limit, in characters
	maxProfileAbout = 139     // likewise
	maxProfilePhoto = 1 << 20 // the UI's 640x640 JPEG is far under it
)

// profileMu guards me.jpg: a fetch and a change of the picture don't interleave.
// ponytail: one lock for both accounts' picture work; they never run together.
var profileMu sync.Mutex

func profilePath(dir string) string { return filepath.Join(dir, "profile", "me.jpg") }

// profilePicture is me.jpg's path when it exists, else "".
func profilePicture(dir string) string {
	if p, err := filepath.Abs(profilePath(dir)); err == nil {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

type profileInfo struct {
	Name    string `json:"name"`
	About   string `json:"about"`
	Phone   string `json:"phone"`
	Picture string `json:"picture"`
}

// profileSet is a checked profile.set: the pointers are the fields that came.
type profileSet struct {
	name, about *string
	photo       bool   // a picture came: jpeg is it, nil for "remove"
	jpeg        []byte // at most 1 MB: small, so read whole
}

// parseProfileSet checks profile.set's fields, as the real and the simulated account do.
func parseProfileSet(req json.RawMessage) (profileSet, error) {
	var r struct{ Name, About, Picture *string }
	var out profileSet
	if err := json.Unmarshal(req, &r); err != nil || r.Name == nil && r.About == nil && r.Picture == nil {
		return out, &Error{"bad_request", "profile.set needs name, about or picture"}
	}
	if r.Name != nil {
		name := strings.TrimSpace(*r.Name)
		if n := utf8.RuneCountInString(name); n < 1 || n > maxProfileName {
			return out, &Error{"bad_request", "profile.set: name is 1 to 25 characters"}
		}
		out.name = &name
	}
	if r.About != nil {
		if utf8.RuneCountInString(*r.About) > maxProfileAbout {
			return out, &Error{"bad_request", "profile.set: about is at most 139 characters"}
		}
		out.about = r.About
	}
	if r.Picture != nil {
		out.photo = true
		if *r.Picture != "" {
			data, err := readProfileJPEG(*r.Picture)
			if err != nil {
				return out, err
			}
			out.jpeg = data
		}
	}
	return out, nil
}

// readProfileJPEG reads the picture at path: absolute, a JPEG, at most 1 MB.
func readProfileJPEG(path string) ([]byte, error) {
	bad := func(why string) error { return &Error{"bad_request", "profile.set: picture " + why + ": " + path} }
	if !filepath.IsAbs(path) {
		return nil, bad("isn't an absolute path")
	}
	fi, err := os.Stat(path)
	switch {
	case err != nil || !fi.Mode().IsRegular():
		return nil, bad("isn't a file")
	case fi.Size() > maxProfilePhoto:
		return nil, bad("is over 1 MB")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, bad("can't be read")
	}
	if len(data) < 3 || data[0] != 0xFF || data[1] != 0xD8 || data[2] != 0xFF {
		return nil, bad("isn't a JPEG")
	}
	return data, nil
}

// setProfilePhoto makes me.jpg the picture, or removes it for nil. Call it with profileMu held.
func setProfilePhoto(dir string, jpeg []byte) error {
	if jpeg == nil {
		if err := os.Remove(profilePath(dir)); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return writeFile(profilePath(dir), jpeg)
}

func registerProfile(srv *Server, s *Session) {
	srv.Handle("profile.get", s.handleProfileGet)
	srv.Handle("profile.set", s.handleProfileSet)
}

// me returns the client of a linked device and our phone-number JID.
func (s *Session) me() (*whatsmeow.Client, types.JID, error) {
	cli := s.client()
	if cli.Store.ID == nil {
		return nil, types.EmptyJID, &Error{"unlinked", "link this device first"}
	}
	return cli, cli.Store.ID.ToNonAD(), nil
}

func (s *Session) handleProfileGet(json.RawMessage) (any, error) {
	cli, me, err := s.me()
	if err != nil {
		return nil, err
	}
	out := profileInfo{Name: cli.Store.PushName, Phone: "+" + me.User}
	ctx, cancel := context.WithTimeout(context.Background(), netTimeout)
	defer cancel()
	infos, err := cli.GetUserInfo(ctx, []types.JID{me})
	if err != nil {
		return nil, err
	}
	for _, i := range infos {
		out.About = i.Status
	}
	if out.Picture = profilePicture(s.srv.dir); out.Picture == "" {
		// A failure only logs: the name, number and about still answer.
		if p, err := s.fetchProfilePicture(cli, me); err != nil {
			s.log.Warnf("profile.get: picture: %v", err)
		} else {
			out.Picture = p
		}
	}
	return out, nil
}

// fetchProfilePicture streams our picture from its URL into me.jpg and returns its path, or ""
// when none is set.
func (s *Session) fetchProfilePicture(cli *whatsmeow.Client, me types.JID) (string, error) {
	profileMu.Lock()
	defer profileMu.Unlock()
	if p := profilePicture(s.srv.dir); p != "" {
		return p, nil // another get got there first
	}
	ctx, cancel := context.WithTimeout(context.Background(), netTimeout)
	defer cancel()
	info, err := cli.GetProfilePictureInfo(ctx, me, &whatsmeow.GetProfilePictureParams{})
	if errors.Is(err, whatsmeow.ErrProfilePictureNotSet) || errors.Is(err, whatsmeow.ErrProfilePictureUnauthorized) ||
		err == nil && (info == nil || info.URL == "") {
		return "", nil
	} else if err != nil {
		return "", err
	}
	dst, err := filepath.Abs(profilePath(s.srv.dir))
	if err != nil {
		return "", err
	}
	return downloadTo(strings.TrimSuffix(dst, ".jpg")+".tmp", streamURL(ctx, info.URL), func(string) string { return dst })
}

func (s *Session) handleProfileSet(req json.RawMessage) (any, error) {
	r, err := parseProfileSet(req)
	if err != nil {
		return nil, err
	}
	cli, _, err := s.me()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if r.name != nil {
		if err := cli.SendAppState(ctx, appstate.BuildSettingPushName(*r.name)); err != nil {
			return nil, err
		}
		cli.Store.PushName = *r.name // the echo would set it too, later
		if err := cli.Store.Save(ctx); err != nil {
			s.log.Warnf("profile.set: saving the device store: %v", err)
		}
	}
	if r.about != nil {
		if err := cli.SetStatusMessage(ctx, types.SetStatusInput{Text: r.about}); err != nil {
			return nil, err
		}
	}
	if r.photo {
		profileMu.Lock()
		defer profileMu.Unlock()
		// Our own photo goes through the group-photo IQ with no target, which WhatsApp reads as
		// us. ponytail: unverified until the real link.
		if _, err := cli.SetGroupPhoto(ctx, types.EmptyJID, r.jpeg); err != nil {
			return nil, err
		}
		if err := setProfilePhoto(s.srv.dir, r.jpeg); err != nil {
			return nil, err
		}
	}
	return nil, nil
}
