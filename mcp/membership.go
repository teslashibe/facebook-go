package mcp

import (
	"context"

	"github.com/teslashibe/facebook-go/groups"
	"github.com/teslashibe/mcptool"
)

// GetMembershipQuestionsInput is the typed input for facebook_get_membership_questions.
type GetMembershipQuestionsInput struct {
	GroupID string `json:"group_id" jsonschema:"description=numeric Facebook group ID,required"`
}

func getMembershipQuestions(ctx context.Context, c *groups.Client, in GetMembershipQuestionsInput) (any, error) {
	res, err := c.GetMembershipQuestions(ctx, in.GroupID)
	if err != nil {
		return nil, err
	}
	return mcptool.PageOf(res, "", 0), nil
}

// MembershipAnswerInput mirrors groups.MembershipAnswer for typed JSON input.
type MembershipAnswerInput struct {
	QuestionID string `json:"question_id" jsonschema:"description=question ID returned by facebook_get_membership_questions,required"`
	Text       string `json:"text,omitempty" jsonschema:"description=free-text answer for OPEN_ENDED questions"`
	Choice     string `json:"choice,omitempty" jsonschema:"description=selected option for MULTIPLE_CHOICE questions; must match one of the question's options"`
}

// JoinGroupWithAnswersInput is the typed input for facebook_join_group_with_answers.
type JoinGroupWithAnswersInput struct {
	GroupID string                  `json:"group_id" jsonschema:"description=numeric Facebook group ID to join,required"`
	Answers []MembershipAnswerInput `json:"answers" jsonschema:"description=one answer per required question from facebook_get_membership_questions,required"`
}

func joinGroupWithAnswers(ctx context.Context, c *groups.Client, in JoinGroupWithAnswersInput) (any, error) {
	answers := make([]groups.MembershipAnswer, 0, len(in.Answers))
	for _, a := range in.Answers {
		answers = append(answers, groups.MembershipAnswer{
			QuestionID: a.QuestionID,
			Text:       a.Text,
			Choice:     a.Choice,
		})
	}
	if err := c.JoinGroupWithAnswers(ctx, in.GroupID, answers); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "group_id": in.GroupID}, nil
}

var membershipTools = []mcptool.Tool{
	mcptool.Define[*groups.Client, GetMembershipQuestionsInput](
		"facebook_get_membership_questions",
		"List the membership questions a closed Facebook group asks prospective members",
		"GetMembershipQuestions",
		getMembershipQuestions,
	),
	mcptool.Define[*groups.Client, JoinGroupWithAnswersInput](
		"facebook_join_group_with_answers",
		"Submit a join request for a gated Facebook group together with answers to its membership questions",
		"JoinGroupWithAnswers",
		joinGroupWithAnswers,
	),
}
