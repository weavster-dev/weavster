---
description: Brainstorm on a feature with the goal of generating a feature plan
allowedTools:
  - "Read"
  - "Write"
  - "Glob"
  - "Grep"
  - "Bash(ls:*)"
---

# Feature Brainstorm

I've got an idea I want to talk through with you. I'd like you to help me turn it into a fully formed plan, which will eventually include implementation details.

Start by asking me what we're working on.

Next, check out the current state of the project in our working directory to understand where we're starting off.

You should then ask me questions, one at a time, to help refine the idea.

Ideally, the questions would be multiple choice, but open-ended questions are OK, too. Don't forget: only one question per message.

Once you believe you understand what we're doing, stop and describe the design to me, in sections of maybe 200-300 words at a time, asking after each section whether it looks right so far.

Finally, once you have a plan in mind, save it to the `/plans` folder with a timestamped filename: `yyyyMMddhhmmss_feature_name.md`

## Plan Structure

The plan should include:

1. **Overview** - What we're building and why
2. **Scope** - What's included and explicitly excluded
3. **Implementation Tasks** - Broken down steps
4. **Documentation Needs** - What docs need updating
5. **Testing Strategy** - How we'll verify it works
6. **Open Questions** - Anything still to be decided

## Next Steps

After the plan is saved, suggest running `/create-from-plan` to:
- Create GitHub issue(s) from the plan
- Determine if this is a Feature (1 issue) or Epic (tracking issue + sub-issues)
- Link the plan file to the created issue(s)
- Add everything to the project board
