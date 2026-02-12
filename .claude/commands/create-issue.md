---
description: Create a GitHub issue
allowedTools:
  - "Bash(gh issue:*)"
---

# Create Issue

I'll help you create a GitHub issue for the Weavster project. Tell me what you want to create an issue for.

Once you provide the description, I will:

1. **Analyze your request** to determine the issue type:
   - **Bug**: Something is broken or not working correctly
   - **Feature**: New functionality or improvement to existing features
   - **Docs**: Missing or unclear documentation

2. **Intelligently fill the template** based on your description:

   **Bug Template:**
   - Title: Concise description
   - Labels: `bug`
   - Sections: Description, Steps to Reproduce, Configuration (if relevant), Error Output, Version, OS

   **Feature Template:**
   - Title: Concise description
   - Labels: `enhancement`
   - Sections: Description, Use Case, Proposed Solution, Affected Crate(s)

   **Docs Template:**
   - Title: Concise description
   - Labels: `documentation`
   - Sections: Summary, What's Missing or Unclear, Proposed Content, Location

3. **Create the issue** using the GitHub CLI with appropriate labels

4. **Return the issue URL** so you can view it

If the type is ambiguous, I'll ask you to clarify before creating.

**Note:** I will auto-fill as much information as possible from your description and codebase context. Sections without enough info will have `[To be determined]` placeholders.

What would you like to create an issue for?
