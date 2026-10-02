package managedkafka

import "testing"

func TestDeriveACLPatternShapes(t *testing.T) {
	cases := []struct {
		id, typ, name, pattern string
	}{
		{"cluster", "CLUSTER", "kafka-cluster", "LITERAL"},
		{"allTopics", "TOPIC", "*", "LITERAL"},
		{"allConsumerGroups", "GROUP", "*", "LITERAL"},
		{"allTransactionalIds", "TRANSACTIONAL_ID", "*", "LITERAL"},
		{"topicPrefixed/ord", "TOPIC", "ord", "PREFIXED"},
		{"consumerGroupPrefixed/cg", "GROUP", "cg", "PREFIXED"},
		{"transactionalIdPrefixed/tx", "TRANSACTIONAL_ID", "tx", "PREFIXED"},
		{"topic/orders", "TOPIC", "orders", "LITERAL"},
		{"consumerGroup/readers", "GROUP", "readers", "LITERAL"},
		{"transactionalId/tid", "TRANSACTIONAL_ID", "tid", "LITERAL"},
		{"custom-shape", "", "custom-shape", "LITERAL"},
	}
	for _, tc := range cases {
		typ, name, pattern := deriveACLPattern(tc.id)
		if typ != tc.typ || name != tc.name || pattern != tc.pattern {
			t.Fatalf("%s => %q %q %q want %q %q %q", tc.id, typ, name, pattern, tc.typ, tc.name, tc.pattern)
		}
	}
}
