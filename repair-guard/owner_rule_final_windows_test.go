package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestOwnerRule4088DefaultMixedStoreWriters(t *testing.T) {
	config := ownerDefaultRuleConfig()
	for _, relative := range []string{
		"note-history-v1/7-fixture.json", "stall_summary.json",
		"capacity-decisions/fixture.json", "todo.before-rename-fixture.json",
		"todo.json.bak-fixture", "todo.before-rename-fixture.json/nested",
	} {
		path := filepath.Join(filepath.Dir(config.todoStore), filepath.FromSlash(relative))
		v, handled := ownerRuleStoredContent(context.Background(), path, object{}, ownerSourceAccess{}, config)
		if !handled || v.AccessAllowed || v.Basis != "mixed_client_record_store" || !ownerRuleRecordedPath(path, config) {
			t.Fatal("known mixed-store writer output escaped protection")
		}
	}
	for _, name := range []string{"todo.exe", "todo.py", "todo-helper.json"} {
		path := filepath.Join(filepath.Dir(config.todoStore), name)
		if _, handled := ownerRuleStoredContent(context.Background(), path, object{}, ownerSourceAccess{}, config); handled || ownerRuleRecordedPath(path, config) || !ownerRuleInfrastructure(path, config) {
			t.Fatal("table-copy rule captured an unrelated operational tool")
		}
	}
}

func TestOwnerRule4088PrivateRegistryCopiesPrecedeSnapshot(t *testing.T) {
	config := ownerDefaultRuleConfig()
	for _, suffix := range []string{".bak", ".backup", ".old", ".orig"} {
		path := config.records[1] + suffix
		v := ownerOrganizationQueryWith(context.Background(), path, ownerSourceAccess{}, config, func(context.Context, func(io.Reader) (object, bool)) (*ownerProjectSnapshot, bool) {
			t.Error("private registry copy reached snapshot reader")
			return nil, false
		})
		if v.AccessAllowed || v.Status != "protected" || v.Basis != "client_record_store" {
			t.Fatal("private registry copy reached ordinary infrastructure classification")
		}
	}
}

func TestOwnerRule4088FinalizationKeepsOnlyIndependentQualifications(t *testing.T) {
	f := newConversationFixture(t)
	f.publish()
	conversation := func(p string) bool { return conversationAllowed(p, f.a) }
	tool := filepath.Join(f.a.home, "discord_ask.exe")
	if err := os.WriteFile(tool, []byte("synthetic runtime"), 0600); err != nil {
		t.Fatal("fixture runtime")
	}
	operational := func(p string) bool { return discordAskRuntimeIn(p, f.a.home, func(string) bool { return false }) }
	noReceipt := func(string) bool { t.Fatal("mixed store reached generic receipt"); return true }
	for _, basis := range []string{"registration_snapshot_unavailable", "registration_snapshot_changed", "classification_deadline"} {
		v := ownerRuleUnknown(basis)
		v.ReceiptRestricted = true
		if got := ownerRuleFinalize(v, f.replyPath, conversation, operational, noReceipt); !got.AccessAllowed || got.Basis != "verified_conversation_record" {
			t.Fatal("independent conversation qualification lost on transient failure")
		}
		if got := ownerRuleFinalize(v, tool, conversation, operational, noReceipt); !got.AccessAllowed || got.Basis != "verified_operational_receipt" {
			t.Fatal("independent installed runtime lost on transient failure")
		}
		if got := ownerRuleFinalize(v, filepath.Join(f.a.home, "body.txt"), conversation, operational, noReceipt); got.AccessAllowed {
			t.Fatal("unknown mixed body acquired independent qualification")
		}
	}
	never := func(string) bool { t.Fatal("positive protection reached a qualification"); return true }
	positive := ownerRuleProtected("client_registration")
	positive.ReceiptRestricted = true
	if ownerRuleFinalize(positive, tool, never, never, never).AccessAllowed {
		t.Fatal("client protection lost to an independent runtime")
	}
	conflict := ownerRuleUnknown("owner_location_conflict")
	conflict.ReceiptRestricted = true
	if ownerRuleFinalize(conflict, tool, never, never, noReceipt).AccessAllowed {
		t.Fatal("conflicting runtime acquired transient qualification")
	}
	ordinary := ownerRuleUnknown("registration_snapshot_unavailable")
	if !ownerRuleFinalize(ordinary, "fixture", never, never, func(string) bool { return true }).AccessAllowed {
		t.Fatal("ordinary independent receipt changed")
	}
}
