package core

import (
	"mittere/internal/lib/memrepo"
	"testing"
)

// The channel count in a topic listing is what a message to that topic would
// reach, so a disabled channel must not make an empty topic look populated.
func TestGetTopicsCountsActiveChannels(t *testing.T) {
	repo := memrepo.New().WithTopic("ops", 111, 222).WithTopic("quiet", 333).WithTopic("empty")
	disable(repo, 333)

	tests := []struct {
		topic string
		want  int
	}{
		{"ops", 2},
		{"quiet", 0},
		{"empty", 0},
	}

	core := testCore(repo, nil)

	topics, err := core.GetTopics()
	if err != nil {
		t.Fatalf("GetTopics() error = %v", err)
	}
	if len(topics) != len(tests) {
		t.Fatalf("GetTopics() returned %d topics, want %d", len(topics), len(tests))
	}
	counts := make(map[string]int, len(topics))
	for _, topic := range topics {
		counts[topic.Code] = topic.Channels
	}

	for _, tt := range tests {
		t.Run(tt.topic, func(t *testing.T) {
			if counts[tt.topic] != tt.want {
				t.Errorf("GetTopics() channels of %s = %d, want %d", tt.topic, counts[tt.topic], tt.want)
			}
			topic, err := core.GetTopic(tt.topic)
			if err != nil {
				t.Fatalf("GetTopic(%s) error = %v", tt.topic, err)
			}
			if topic == nil {
				t.Fatalf("GetTopic(%s) = nil", tt.topic)
			}
			if topic.Channels != tt.want {
				t.Errorf("GetTopic(%s).Channels = %d, want %d", tt.topic, topic.Channels, tt.want)
			}
		})
	}
}

func TestGetTopicUnknown(t *testing.T) {
	core := testCore(memrepo.New(), nil)
	topic, err := core.GetTopic("nowhere")
	if err != nil {
		t.Fatalf("GetTopic() error = %v", err)
	}
	if topic != nil {
		t.Errorf("GetTopic() = %v, want nil for an unknown topic", topic)
	}
}
