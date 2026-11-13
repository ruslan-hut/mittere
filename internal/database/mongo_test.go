package database

import (
	"context"
	"errors"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"mittere/entity"
	"os"
	"testing"
	"time"
)

// testDB connects to a real MongoDB, as the integration tests of this project
// do; they are skipped by go test -short. The address comes from MONGO_URI.
func testDB(t *testing.T) *MongoDB {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping the integration test with -short")
	}
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		uri = "mongodb://127.0.0.1:27017"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Skipf("no mongodb at %s: %v", uri, err)
	}
	if err = client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		t.Skipf("no mongodb at %s: %v", uri, err)
	}

	name := fmt.Sprintf("mittere_test_%d", time.Now().UnixNano())
	db := NewMongoDB(client, name, 90*24*time.Hour)
	t.Cleanup(func() {
		_ = client.Database(name).Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	return db
}

func prepare(t *testing.T, db *MongoDB) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if err := db.Prepare(ctx); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if err := db.EnsureDefaults(); err != nil {
		t.Fatalf("EnsureDefaults() error = %v", err)
	}
}

// TestMigrateFromV1 covers the move away from the single subscriptions
// collection, where subscriber, recipient and sender were one user.
func TestMigrateFromV1(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	legacy := []interface{}{
		entity.User{UserID: 111, Username: "johndoe", Name: "John", Role: entity.RoleUser,
			State: entity.StateActive, IsVerified: true, SubscriptionType: "status"},
		entity.User{UserID: 222, Username: "boss", Role: entity.RoleAdmin,
			State: entity.StateActive, IsVerified: true, SubscriptionType: "metrics"},
		entity.User{Username: "ci-bot", Token: "ci-token", Role: entity.RoleUser, State: entity.StateActive},
		entity.User{UserID: 333, Username: "left", State: entity.StateDisabled, SubscriptionType: "status"},
	}
	if _, err := db.collection(subscriptionsCollection).InsertMany(ctx, legacy); err != nil {
		t.Fatalf("seeding v1 data: %v", err)
	}

	report, err := db.Migrate(ctx)
	if err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if !report.Applied || report.Users != 4 {
		t.Fatalf("report = %+v, want 4 migrated users", report)
	}
	if report.Channels != 3 || report.Senders != 1 {
		t.Errorf("report = %+v, want 3 channels and 1 sender", report)
	}

	// the v1 documents are kept aside, nothing is thrown away
	count, err := db.collection(legacyUsersCollection).CountDocuments(ctx, bson.D{})
	if err != nil || count != 4 {
		t.Errorf("backup holds %d documents, want 4 (%v)", count, err)
	}
	count, err = db.collection(subscriptionsCollection).CountDocuments(ctx, legacyUserFilter())
	if err != nil || count != 0 {
		t.Errorf("%d v1 documents left in subscriptions (%v)", count, err)
	}

	// the indexes only fit the v2 schema, so they go in after the migration
	if err = db.Prepare(ctx); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	channel, err := db.GetChannel(entity.ChannelKey(111, 0))
	if err != nil || channel == nil {
		t.Fatalf("channel of the first user missing: %v", err)
	}
	if channel.Kind != entity.ChannelPrivate || !channel.IsActive() || channel.Username != "johndoe" {
		t.Errorf("channel = %+v, want an active private channel of johndoe", channel)
	}

	// what a subscriber received in v1 it keeps receiving: the untargeted
	// broadcasts through the default topic, its own subscription type, and the
	// admin messages for an admin
	topics, err := db.GetTopicsOfChannel(channel.ChannelID)
	if err != nil {
		t.Fatalf("GetTopicsOfChannel() error = %v", err)
	}
	if !hasAll(topics, entity.TopicDefault, "status") {
		t.Errorf("topics of johndoe = %v, want default and status", topics)
	}
	adminTopics, _ := db.GetTopicsOfChannel(entity.ChannelKey(222, 0))
	if !hasAll(adminTopics, entity.TopicDefault, "metrics", entity.TopicAdmin) {
		t.Errorf("topics of the admin = %v, want default, metrics and admin", adminTopics)
	}

	// the user that only had a token becomes a sender, not a channel
	sender, err := db.GetSenderByToken("ci-token")
	if err != nil || sender == nil {
		t.Fatalf("sender not migrated: %v", err)
	}
	if sender.Scope != entity.ScopeSend || sender.Username != "ci-bot" {
		t.Errorf("sender = %+v, want ci-bot with the send scope", sender)
	}

	// a disabled subscriber keeps its state and receives nothing
	recipients, err := db.GetChannelsByTopic("status")
	if err != nil {
		t.Fatalf("GetChannelsByTopic() error = %v", err)
	}
	if len(recipients) != 1 || recipients[0].ChatID != 111 {
		t.Errorf("recipients of status = %+v, want only the active subscriber", recipients)
	}

	// running again changes nothing
	second, err := db.Migrate(ctx)
	if err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}
	if second.Applied {
		t.Error("the migration ran a second time")
	}
	count, _ = db.collection(legacyUsersCollection).CountDocuments(ctx, bson.D{})
	if count != 4 {
		t.Errorf("backup holds %d documents after the second run, want 4", count)
	}
}

func TestChannelsAndSubscriptions(t *testing.T) {
	db := testDB(t)
	prepare(t, db)

	group := entity.NewChannel(-1001, 0, entity.ChannelGroup)
	group.Title = "Маркетингові відправки"
	forum := entity.NewChannel(-1002, 7, entity.ChannelForum)
	disabled := entity.NewChannel(-1003, 0, entity.ChannelGroup)
	disabled.State = entity.StateDisabled

	topic := entity.NewTopic("marketing.shipments", "Відправки")
	if err := db.CreateTopic(&topic); err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	for _, channel := range []entity.Channel{group, forum, disabled} {
		if err := db.SaveChannel(&channel); err != nil {
			t.Fatalf("SaveChannel() error = %v", err)
		}
		sub := entity.NewSubscription(topic.Code, channel.ChannelID, "test")
		if err := db.Subscribe(&sub); err != nil {
			t.Fatalf("Subscribe() error = %v", err)
		}
		// subscribing twice is not an error and does not duplicate
		if err := db.Subscribe(&sub); err != nil {
			t.Fatalf("second Subscribe() error = %v", err)
		}
	}

	recipients, err := db.GetChannelsByTopic(topic.Code)
	if err != nil {
		t.Fatalf("GetChannelsByTopic() error = %v", err)
	}
	if len(recipients) != 2 {
		t.Fatalf("recipients = %d, want 2 active ones", len(recipients))
	}
	for _, channel := range recipients {
		if channel.ChatID == -1002 && channel.ThreadID != 7 {
			t.Errorf("forum channel lost its thread: %+v", channel)
		}
	}

	// a forum topic is a channel of its own, next to the group it lives in
	same := entity.NewChannel(-1002, 0, entity.ChannelGroup)
	if same.ChannelID == forum.ChannelID {
		t.Error("the group and its forum topic share an identifier")
	}

	if err = db.SetTopicsOfChannel(group.ChannelID, []string{"marketing.shipments", entity.TopicDefault}, "test"); err != nil {
		t.Fatalf("SetTopicsOfChannel() error = %v", err)
	}
	topics, err := db.GetTopicsOfChannel(group.ChannelID)
	if err != nil || !hasAll(topics, "marketing.shipments", entity.TopicDefault) {
		t.Errorf("topics = %v, want both (%v)", topics, err)
	}

	if err = db.DeleteChannel(forum.ChannelID); err != nil {
		t.Fatalf("DeleteChannel() error = %v", err)
	}
	left, _ := db.GetTopicsOfChannel(forum.ChannelID)
	if len(left) != 0 {
		t.Errorf("subscriptions of a deleted channel are left behind: %v", left)
	}
}

func TestEventsJournal(t *testing.T) {
	db := testDB(t)
	prepare(t, db)

	now := time.Now().UTC()
	event := &entity.Event{
		EventID:   "ev1",
		Topic:     "marketing.shipments",
		Type:      "alert",
		Text:      "накладна 42",
		Sender:    "1c",
		DedupKey:  "invoice-42",
		CreatedAt: now,
		Deliveries: []entity.Delivery{
			{ChannelID: "-1001", ChatID: -1001, Status: entity.DeliverySent, Attempts: 1, MessageID: 5, SentAt: &now},
			{ChannelID: "-1002", ChatID: -1002, Status: entity.DeliveryFailed, Attempts: 2, Error: "chat not found"},
		},
	}
	event.Recalculate()
	if event.Status != entity.EventPartial {
		t.Fatalf("status = %q, want %q", event.Status, entity.EventPartial)
	}
	if err := db.SaveEvent(event); err != nil {
		t.Fatalf("SaveEvent() error = %v", err)
	}

	stored, err := db.GetEvent("ev1")
	if err != nil || stored == nil {
		t.Fatalf("GetEvent() = %v, %v", stored, err)
	}
	if len(stored.Deliveries) != 2 || stored.Deliveries[1].Error != "chat not found" {
		t.Errorf("stored event = %+v, want both deliveries with the failure reason", stored)
	}

	byKey, err := db.GetEventByDedupKey("invoice-42")
	if err != nil || byKey == nil || byKey.EventID != "ev1" {
		t.Errorf("GetEventByDedupKey() = %v, %v", byKey, err)
	}
	missing, err := db.GetEventByDedupKey("invoice-43")
	if err != nil || missing != nil {
		t.Errorf("GetEventByDedupKey() for an unknown key = %v, %v", missing, err)
	}

	old := &entity.Event{EventID: "ev0", Topic: "ops", Status: entity.EventDelivered,
		CreatedAt: now.Add(-48 * time.Hour)}
	if err = db.SaveEvent(old); err != nil {
		t.Fatalf("SaveEvent() error = %v", err)
	}

	found, err := db.FindEvents(entity.EventFilter{Topic: "marketing.shipments"})
	if err != nil || len(found) != 1 || found[0].EventID != "ev1" {
		t.Errorf("filter by topic returned %+v (%v)", found, err)
	}
	found, err = db.FindEvents(entity.EventFilter{Status: entity.EventPartial})
	if err != nil || len(found) != 1 {
		t.Errorf("filter by status returned %+v (%v)", found, err)
	}
	found, err = db.FindEvents(entity.EventFilter{From: now.Add(-time.Hour)})
	if err != nil || len(found) != 1 || found[0].EventID != "ev1" {
		t.Errorf("filter by time returned %+v (%v)", found, err)
	}
	found, err = db.FindEvents(entity.EventFilter{})
	if err != nil || len(found) != 2 {
		t.Errorf("unfiltered search returned %d events (%v)", len(found), err)
	}
	if len(found) == 2 && found[0].EventID != "ev1" {
		t.Error("events are not returned newest first")
	}

	// the journal is kept for 90 days by a TTL index
	cursor, err := db.collection(eventsCollection).Indexes().List(context.Background())
	if err != nil {
		t.Fatalf("listing indexes: %v", err)
	}
	var indexes []bson.M
	if err = cursor.All(context.Background(), &indexes); err != nil {
		t.Fatalf("reading indexes: %v", err)
	}
	ttl := false
	for _, index := range indexes {
		if index["name"] == "events_ttl" && index["expireAfterSeconds"] != nil {
			ttl = true
		}
	}
	if !ttl {
		t.Errorf("no TTL index on the journal: %v", indexes)
	}
}

func TestLinkCodes(t *testing.T) {
	db := testDB(t)
	prepare(t, db)

	now := time.Now().UTC()
	code := &entity.LinkCode{
		Code:      "ABCD-1234",
		Topic:     "marketing.shipments",
		CreatedAt: now,
		ExpiresAt: now.Add(30 * time.Minute),
		CreatedBy: "system",
	}
	if err := db.CreateLinkCode(code); err != nil {
		t.Fatalf("CreateLinkCode() error = %v", err)
	}

	stored, err := db.GetLinkCode("ABCD-1234")
	if err != nil || stored == nil {
		t.Fatalf("GetLinkCode() = %v, %v", stored, err)
	}
	if !stored.IsUsable(now) {
		t.Error("a fresh code is not usable")
	}
	if err = db.UseLinkCode("ABCD-1234", "-1001", now); err != nil {
		t.Fatalf("UseLinkCode() error = %v", err)
	}
	if err = db.UseLinkCode("ABCD-1234", "-1002", now); err == nil {
		t.Error("the same code was spent twice")
	}

	expired := &entity.LinkCode{Code: "EXPI-RED", CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(-time.Minute)}
	if err = db.CreateLinkCode(expired); err != nil {
		t.Fatalf("CreateLinkCode() error = %v", err)
	}
	if err = db.UseLinkCode("EXPI-RED", "-1003", now); err == nil {
		t.Error("an expired code was accepted")
	}
}

func TestSendersRoundTrip(t *testing.T) {
	db := testDB(t)
	prepare(t, db)

	sender := entity.NewSender("1c", entity.ScopeSend)
	sender.Token = "token-1c"
	sender.AllowedTopics = []string{"marketing.shipments"}
	if err := db.CreateSender(&sender); err != nil {
		t.Fatalf("CreateSender() error = %v", err)
	}
	if err := db.CreateSender(&sender); err == nil {
		t.Error("the same username was taken twice")
	}

	found, err := db.GetSenderByToken("token-1c")
	if err != nil || found == nil {
		t.Fatalf("GetSenderByToken() = %v, %v", found, err)
	}
	if !found.MayPublish("marketing.shipments") || found.MayPublish("ops") {
		t.Errorf("sender = %+v, want publishing limited to its topic", found)
	}

	found.State = entity.StateDisabled
	if err = db.UpdateSender(found); err != nil {
		t.Fatalf("UpdateSender() error = %v", err)
	}
	again, _ := db.GetSender("1c")
	if again.IsActive() {
		t.Error("the sender is still active after being disabled")
	}
	if err = db.DeleteSender("1c"); err != nil {
		t.Fatalf("DeleteSender() error = %v", err)
	}
	if err = db.DeleteSender("1c"); err == nil {
		t.Error("deleting a missing sender passed")
	}
}

func hasAll(got []string, want ...string) bool {
	set := make(map[string]bool, len(got))
	for _, value := range got {
		set[value] = true
	}
	for _, value := range want {
		if !set[value] {
			return false
		}
	}
	return len(got) == len(want)
}

// TestClaimEvent covers the guard against sending the same message twice: a
// second call with the same dedup key is turned away while the first is still
// delivering, and a claim left by a crashed call is taken over once it expires.
func TestClaimEvent(t *testing.T) {
	db := testDB(t)
	prepare(t, db)

	first := &entity.Event{EventID: "ev1", Topic: "ops", Type: "alert",
		DedupKey: "invoice-42", Status: entity.EventPending, CreatedAt: time.Now().UTC()}
	previous, err := db.ClaimEvent(first, time.Minute)
	if err != nil || previous != nil {
		t.Fatalf("ClaimEvent() = %v, %v, want a fresh claim", previous, err)
	}

	second := &entity.Event{EventID: "ev2", Topic: "ops", Type: "alert",
		DedupKey: "invoice-42", Status: entity.EventPending, CreatedAt: time.Now().UTC()}
	if _, err = db.ClaimEvent(second, time.Minute); !errors.Is(err, entity.ErrInProgress) {
		t.Errorf("ClaimEvent() error = %v, want %v", err, entity.ErrInProgress)
	}

	// the first call finishes and releases the claim
	sentAt := time.Now().UTC()
	first.Deliveries = []entity.Delivery{{ChannelID: "-1001", ChatID: -1001,
		Status: entity.DeliverySent, Attempts: 1, MessageID: 5, SentAt: &sentAt}}
	first.Recalculate()
	first.LeaseUntil = nil
	if err = db.SaveEvent(first); err != nil {
		t.Fatalf("SaveEvent() error = %v", err)
	}

	third := &entity.Event{EventID: "ev3", Topic: "ops", Type: "alert",
		DedupKey: "invoice-42", Status: entity.EventPending, CreatedAt: time.Now().UTC()}
	previous, err = db.ClaimEvent(third, time.Minute)
	if err != nil {
		t.Fatalf("ClaimEvent() error = %v", err)
	}
	if previous == nil || previous.EventID != "ev1" || len(previous.Deliveries) != 1 {
		t.Fatalf("ClaimEvent() = %+v, want what the first call left behind", previous)
	}

	// a claim that expired is taken over, so a crash does not block the key
	expired := &entity.Event{EventID: "ev4", Topic: "ops", DedupKey: "invoice-43",
		Status: entity.EventPending, CreatedAt: time.Now().UTC()}
	if _, err = db.ClaimEvent(expired, -time.Second); err != nil {
		t.Fatalf("ClaimEvent() error = %v", err)
	}
	taken := &entity.Event{EventID: "ev5", Topic: "ops", DedupKey: "invoice-43",
		Status: entity.EventPending, CreatedAt: time.Now().UTC()}
	previous, err = db.ClaimEvent(taken, time.Minute)
	if err != nil || previous == nil || previous.EventID != "ev4" {
		t.Errorf("ClaimEvent() = %+v, %v, want the expired claim taken over", previous, err)
	}

	// an event without a dedup key is never held up
	for _, id := range []string{"plain1", "plain2"} {
		plain := &entity.Event{EventID: id, Topic: "ops", Status: entity.EventPending, CreatedAt: time.Now().UTC()}
		if _, err = db.ClaimEvent(plain, time.Minute); err != nil {
			t.Errorf("ClaimEvent() for an event without a key: %v", err)
		}
	}
}

// TestPrepareSurvivesChangedTTL covers a changed events.ttl_days: the index is
// rebuilt instead of stopping the service from starting.
func TestPrepareSurvivesChangedTTL(t *testing.T) {
	db := testDB(t)
	prepare(t, db)

	db.eventsTTL = 30 * 24 * time.Hour
	if err := db.Prepare(context.Background()); err != nil {
		t.Fatalf("Prepare() with a changed TTL: %v", err)
	}

	cursor, err := db.collection(eventsCollection).Indexes().List(context.Background())
	if err != nil {
		t.Fatalf("listing indexes: %v", err)
	}
	var indexes []bson.M
	if err = cursor.All(context.Background(), &indexes); err != nil {
		t.Fatalf("reading indexes: %v", err)
	}
	for _, index := range indexes {
		if index["name"] == "events_ttl" {
			if got := index["expireAfterSeconds"]; fmt.Sprint(got) != "2592000" {
				t.Errorf("expireAfterSeconds = %v, want 2592000", got)
			}
			return
		}
	}
	t.Error("the TTL index is gone")
}

// TestMigrateKeepsLegacyDocumentsUntilTheEnd covers the order the migration
// works in: the new links are written before the v1 documents are removed, so a
// crash in between leaves something to rebuild from.
func TestMigrateKeepsLegacyDocumentsFaithfully(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// a v1 document with a field the current struct does not know about
	legacy := bson.M{"user_id": int64(111), "username": "johndoe", "role": entity.RoleUser,
		"state": entity.StateActive, "subscription_type": "status", "legacy_note": "keep me"}
	result, err := db.collection(subscriptionsCollection).InsertOne(ctx, legacy)
	if err != nil {
		t.Fatalf("seeding: %v", err)
	}
	// a token holder that was never activated in Telegram
	if _, err = db.collection(subscriptionsCollection).InsertOne(ctx, entity.User{
		Username: "ci-bot", Token: "ci-token", Role: entity.RoleUser, State: entity.StateAwait,
	}); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	if _, err = db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	var backup bson.M
	if err = db.collection(legacyUsersCollection).FindOne(ctx, bson.M{"username": "johndoe"}).Decode(&backup); err != nil {
		t.Fatalf("reading the backup: %v", err)
	}
	if backup["legacy_note"] != "keep me" {
		t.Errorf("the backup dropped a v1 field: %v", backup)
	}
	if backup["_id"] != result.InsertedID {
		t.Errorf("the backup changed the identifier: %v, want %v", backup["_id"], result.InsertedID)
	}

	// v1 authenticated a token whatever the state of its user, so the migrated
	// sender must not start out locked
	sender, err := db.GetSenderByToken("ci-token")
	if err != nil || sender == nil {
		t.Fatalf("sender not migrated: %v", err)
	}
	if !sender.IsActive() {
		t.Errorf("sender = %+v, want it active so its token keeps working", sender)
	}
}
