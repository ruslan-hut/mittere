package core

import (
	"io"
	"log/slog"
	"mittere/entity"
	"mittere/internal/lib/memrepo"
	"strings"
	"testing"
	"time"
)

func chatCore(repo *memrepo.Repo) *Core {
	return New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func issueCode(t *testing.T, repo *memrepo.Repo, topic string) string {
	t.Helper()
	now := time.Now().UTC()
	code := &entity.LinkCode{Code: "ABCD-1234", Topic: topic, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := repo.CreateLinkCode(code); err != nil {
		t.Fatalf("CreateLinkCode() error = %v", err)
	}
	return code.Code
}

// TestStopInGroupIsRestricted covers an accidental or careless stop in a group:
// switching off the notifications a whole chat relies on is left to the person
// who linked it.
func TestStopInGroupIsRestricted(t *testing.T) {
	repo := memrepo.New().WithTopic("marketing.shipments")
	c := chatCore(repo)
	code := issueCode(t, repo, "marketing.shipments")

	linker := &entity.ChatInfo{ChatID: -1001, Kind: entity.ChannelGroup, Title: "Відправки",
		ActorID: 777, ActorUsername: "ruslan"}
	if reply := c.LinkChat(linker, code); !strings.Contains(reply, "Linked") {
		t.Fatalf("link reply = %q", reply)
	}

	stranger := &entity.ChatInfo{ChatID: -1001, Kind: entity.ChannelGroup, ActorID: 888, ActorUsername: "someone"}
	reply := c.StopChat(stranger)
	if !strings.Contains(reply, "ruslan") {
		t.Errorf("stop reply = %q, want it to name who may stop the chat", reply)
	}
	channel, _ := repo.GetChannel(stranger.ChannelID())
	if !channel.IsActive() {
		t.Error("a stranger switched off the notifications of the group")
	}

	if reply = c.StopChat(linker); !strings.Contains(reply, "stopped") {
		t.Errorf("stop reply for the linker = %q", reply)
	}
	channel, _ = repo.GetChannel(linker.ChannelID())
	if channel.IsActive() {
		t.Error("the person who linked the chat could not stop it")
	}
}

// TestStopInPrivateChatIsAllowed: a private chat belongs to its owner.
func TestStopInPrivateChatIsAllowed(t *testing.T) {
	repo := memrepo.New().WithTopic("status")
	c := chatCore(repo)
	code := issueCode(t, repo, "status")

	chat := &entity.ChatInfo{ChatID: 555, Kind: entity.ChannelPrivate, ActorID: 555, ActorUsername: "johndoe"}
	if reply := c.LinkChat(chat, code); !strings.Contains(reply, "Linked") {
		t.Fatalf("link reply = %q", reply)
	}
	if reply := c.StopChat(chat); !strings.Contains(reply, "stopped") {
		t.Errorf("stop reply = %q", reply)
	}
	channel, _ := repo.GetChannel(chat.ChannelID())
	if channel.IsActive() {
		t.Error("the owner of the private chat could not stop it")
	}
}

// TestLinkChatRejectsSpentCode covers a code that was already used or has
// expired.
func TestLinkChatRejectsSpentCode(t *testing.T) {
	repo := memrepo.New().WithTopic("status")
	c := chatCore(repo)
	code := issueCode(t, repo, "status")

	first := &entity.ChatInfo{ChatID: -1001, Kind: entity.ChannelGroup, ActorID: 1}
	if reply := c.LinkChat(first, code); !strings.Contains(reply, "Linked") {
		t.Fatalf("link reply = %q", reply)
	}
	second := &entity.ChatInfo{ChatID: -1002, Kind: entity.ChannelGroup, ActorID: 2}
	if reply := c.LinkChat(second, code); strings.Contains(reply, "Linked") {
		t.Errorf("a spent code linked a second chat: %q", reply)
	}
	if channel, _ := repo.GetChannel(second.ChannelID()); channel != nil {
		t.Error("a second chat was created from a spent code")
	}

	if reply := c.LinkChat(first, "NOPE-0000"); strings.Contains(reply, "Linked") {
		t.Errorf("an unknown code was accepted: %q", reply)
	}
}

// TestLinkForumTopicIsItsOwnChannel: a topic inside a forum receives on its
// own, next to the group it lives in.
func TestLinkForumTopicIsItsOwnChannel(t *testing.T) {
	repo := memrepo.New().WithTopic("marketing.shipments")
	c := chatCore(repo)

	code := issueCode(t, repo, "marketing.shipments")
	forum := &entity.ChatInfo{ChatID: -1001, ThreadID: 7, Kind: entity.ChannelForum, ActorID: 1}
	if reply := c.LinkChat(forum, code); !strings.Contains(reply, "Linked") {
		t.Fatalf("link reply = %q", reply)
	}

	channel, err := repo.GetChannel(entity.ChannelKey(-1001, 7))
	if err != nil || channel == nil {
		t.Fatalf("forum channel not created: %v", err)
	}
	if channel.ThreadID != 7 || channel.Kind != entity.ChannelForum {
		t.Errorf("channel = %+v, want a forum channel with thread 7", channel)
	}
	if other, _ := repo.GetChannel(entity.ChannelKey(-1001, 0)); other != nil {
		t.Error("linking a forum topic also created a channel for the whole group")
	}
}
