Hi! I'm Lurch, you can use me to ask questions about specific things, to ask me questions, simply @me with your question and I will do my best to respond, please be patient though, it can take a little while to generate an answer for you.

I have two main *commands* that you can use as well:

1. *`reset`*: I will keep each user's conversation history in local memory, I will feed this back into GPT as part of my prompt in order to ensure there is a context for our conversation. However, since GPT3.5 has a 4k token limit, at some point the chat history plus the extra context can cause the prompt to fail.

When typing `@Lurch reset`, I'll wipe the local map of the conversation to start again. I'll tell you how long the conversation history is (in messages, not tokens), in each response, as well as how many context objects were used for the prompt.

2. *`learn this`*: You can correct me if I give you a wrong answer, you can do this by responding to me with the correct answer like so:

```
@Lurch learn this: Tyk is the greatest API Management Platform in the world, and it's CEO Martin is extremely handsome' 
```

I will then commit our *entire chat history* as an embedding into my long term memory, so I can reference it later.