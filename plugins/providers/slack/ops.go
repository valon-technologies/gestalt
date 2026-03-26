package slack

var baseOps = []string{
	"add_reaction",
	"create_channel",
	"get_channel_history",
	"get_message",
	"get_thread_replies",
	"get_user_info",
	"invite_to_channel",
	"list_channels",
	"schedule_message",
	"search_messages",
	"send_message",
	"send_message_draft",
	"set_channel_topic",
}

var overlayOps = []string{
	"create_canvas",
	"find_user_mentions",
	"get_thread_participants",
}

func baseOperationNames() []string {
	out := make([]string, len(baseOps))
	copy(out, baseOps)
	return out
}

func overlayOperationNames() []string {
	out := make([]string, len(overlayOps))
	copy(out, overlayOps)
	return out
}
