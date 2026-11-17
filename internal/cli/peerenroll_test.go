package cli

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"yardr.dev/yardr/internal/store"
)

type interruptedEnrollment struct {
	store.API
	interrupt func()
	err       error
}

func (s interruptedEnrollment) GetPairing(ctx context.Context, id string) (*store.Pairing, error) {
	p, err := s.API.GetPairing(ctx, id)
	if err == nil && p.State == store.PairDone {
		s.interrupt()
		return nil, s.err
	}
	return p, err
}

func (s interruptedEnrollment) PairAnswer(ctx context.Context, id string, yes bool, actor string) (*store.Pairing, error) {
	p, err := s.API.PairAnswer(ctx, id, yes, actor)
	if err == nil && p.State == store.PairDone {
		s.interrupt()
		return nil, s.err
	}
	return p, err
}

func TestPairEnrollmentInterruptedAfterCommit(t *testing.T) {
	for _, fault := range []string{"caller canceled", "host canceled", "host restarted", "host RPC lost", "none"} {
		t.Run(fault, func(t *testing.T) {
			north, laptop := pairYards(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			for _, y := range []*cliYard{north, laptop} {
				if err := y.st.SetSetting(ctx, "name", map[*cliYard]string{north: "north", laptop: "laptop"}[y], "jo"); err != nil {
					t.Fatal(err)
				}
			}
			northInvite, err := invite(ctx, north.a.cfg, north.st, north.srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			laptopInvite, err := invite(ctx, laptop.a.cfg, laptop.st, "")
			if err != nil {
				t.Fatal(err)
			}
			hostPeer, err := north.st.AddPeer(ctx, store.Peer{Name: "laptop", Calls: laptopInvite.Name, Key: laptopInvite.Key}, "jo")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := laptop.st.AddPeer(ctx, store.Peer{Name: "north", Calls: northInvite.Name, Key: northInvite.Key, URL: northInvite.URL}, "jo"); err != nil {
				t.Fatal(err)
			}
			opened, err := north.st.PairOpen(ctx, store.PairOffer{Name: northInvite.Name, Key: northInvite.Key, Enc: northInvite.Enc, URL: northInvite.URL,
				Enrollment: &store.PeerEnrollment{PeerName: hostPeer.Name, Expected: *hostPeer, Invite: laptopInvite}}, "jo")
			if err != nil {
				t.Fatal(err)
			}
			hostCtx, hostCancel := context.WithCancel(ctx)
			defer hostCancel()
			callCtx, callCancel := context.WithCancel(ctx)
			defer callCancel()
			var hostStore store.API = north.st
			if strings.HasPrefix(fault, "host ") {
				interrupt, rpcErr := func() {}, errors.New("connection lost")
				if fault == "host canceled" {
					interrupt, rpcErr = hostCancel, context.Canceled
				} else if fault == "host restarted" {
					rpcErr = store.ErrNotFound
				}
				hostStore = interruptedEnrollment{API: north.st, interrupt: interrupt, err: rpcErr}
			}
			if fault == "caller canceled" {
				old := pairHTTP
				pairHTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					res, err := http.DefaultTransport.RoundTrip(r)
					if err == nil && r.URL.Path == "/peers/pair/confirm" {
						p, perr := north.st.GetPeer(ctx, "laptop")
						if perr == nil && p.Enc != "" {
							res.Body.Close()
							callCancel()
							return nil, context.Canceled
						}
					}
					return res, err
				})}
				defer func() { pairHTTP = old }()
			}
			hosted := make(chan error, 1)
			go func() {
				_, err := north.a.pairHosted(io.Discard, io.Discard, hostCtx, hostStore, opened.Pairing, pairOpts{yes: true})
				hosted <- err
			}()
			callErr := laptop.a.pairJoin(io.Discard, io.Discard, callCtx, laptop.a.cfg, laptop.st, north.srv.URL, opened.Code,
				pairOpts{yes: true, enroll: "north", invite: northInvite.String()})
			hostErr := <-hosted
			p, err := north.st.GetPeer(ctx, "laptop")
			if err != nil || p.Enc != laptopInvite.Enc {
				t.Fatalf("host pin was not committed: %+v, %v", p, err)
			}
			if fault == "none" {
				if callErr != nil || hostErr != nil {
					t.Fatalf("ordinary enrollment: caller %v, host %v", callErr, hostErr)
				}
			} else {
				got := hostErr
				if fault == "caller canceled" {
					got = callErr
				}
				if got == nil || !strings.Contains(got.Error(), "partial or unknown outcome") || !strings.Contains(got.Error(), "Do not remove, re-add") || strings.Contains(got.Error(), nothingSaved) {
					t.Fatalf("interrupted enrollment recovery = %v", got)
				}
			}
		})
	}
}
