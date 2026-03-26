package slackbot

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

func baseOperationNames() []string {
	out := make([]string, len(baseOps))
	copy(out, baseOps)
	return out
}
