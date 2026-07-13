# Admin default request timeout

The admin middleware applied a global 30-second request deadline, which silently
preempted the longer handler budgets for chat, test, and upload operations.

The server now selects one preconstructed timeout guard per route: 120 seconds
by default, four minutes for chat/test, and two minutes for uploads. The five
minute HTTP write timeout remains above every selected request budget.
