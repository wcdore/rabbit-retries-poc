# Claude Configuration for RabbitMQ Retries POC

## Default Mode: PLAN

By default, Claude operates in **PLAN mode** where:
- You can discuss ideas, analyze code, and answer questions
- You can read files and explore the codebase
- You can propose changes and discuss implementation strategies
- **You MUST NOT make any file changes or modifications**
- You MUST NOT create, edit, delete, or write any files
- You should eventually create a checklist for an ACT plan that describes what you will do when you enter ACT mode

## Switching to ACT Mode

Only when explicitly instructed by the user (e.g., "enter ACT mode", "execute the plan", "make the changes", "ACT"), you may enter **ACT mode** where:
- You implement the previously discussed plan
- You make the necessary file changes
- You create, edit, or delete files as needed
- You execute the implementation

## Returning to PLAN Mode

After completing the implementation in ACT mode:
- Automatically return to PLAN mode
- Summarize what was done
- Be ready to discuss further changes without making them

## Mode Indicators

Always be clear about which mode you're in:
- Every message you write should start by identifying in **bold** what mode you're in
- In PLAN mode: Discuss what you *would* do.
- In ACT mode: State that you're making changes and then make them

## Example Workflow

1. User: "How should we implement retry logic?"
2. Claude (PLAN mode): "I would suggest implementing... Here's the plan..."
3. User: "That sounds good, go ahead and implement it"
4. Claude: "Entering ACT mode to implement the discussed changes..."
5. Claude: *makes the changes*
6. Claude: "Changes complete. Returning to PLAN mode. I've implemented..."

Remember: **Always start in PLAN mode** and only make changes when explicitly authorized.
