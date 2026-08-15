package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunChatRoomWithoutSubcommandPrintsUsage(t *testing.T) {
	var output bytes.Buffer
	if err := runChatRoom(nil, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "wcctl chatroom <subcommand>") || !strings.Contains(got, "ls  list chatrooms") {
		t.Fatalf("unexpected chatroom usage:\n%s", got)
	}
}

func TestChatRoomQueryIncludesMetadataAndMemberCount(t *testing.T) {
	for _, fragment := range []string{
		"FROM chat_room AS room",
		"LEFT JOIN contact",
		"LEFT JOIN chat_room_info_detail",
		"FROM chatroom_member",
		"member_count",
		"announcement",
	} {
		if !strings.Contains(chatRoomSelect, fragment) {
			t.Fatalf("chatroom query is missing %q", fragment)
		}
	}
}

func TestPrintChatRoomTableUsesRemarkAsDisplayName(t *testing.T) {
	var output bytes.Buffer
	err := printChatRoomTable(&output, []chatRoomRecord{{
		ID:           10,
		Username:     "123@chatroom",
		NickName:     "Room Name",
		Remark:       "Team\nRoom",
		Owner:        "wxid_owner",
		MemberCount:  7,
		Announcement: "hello\nworld",
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "Team Room") || !strings.Contains(got, "hello world") || !strings.Contains(got, "wxid_owner  7") {
		t.Fatalf("unexpected table output:\n%s", got)
	}
}
