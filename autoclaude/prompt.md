Build this project by working through plan.yaml one task at a time, in order.

For each task:
1. Read plan.yaml and find the first task with status: pending or status: inprogress
2. Set its status to inprogress in plan.yaml
3. Implement the task fully using the /golang skill, including tests as described in the task's `testing` field
4. Set its status to done in plan.yaml
5. Commit all changes to git with a message describing the task that was completed
6. Stop — autoclaude will start a new session for the next task

Do not skip tasks or work on multiple tasks at once. If a task has unmet dependencies (depends_on), check that those tasks are already done before starting.
