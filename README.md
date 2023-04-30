# Lurch - Your Friendly ChatGPT-Powered Slackbot

Lurch is a ChatGPT slackbot for Slack that can make use of embeddings in a Pinecone database.

It is built with the [`botMaker`](https://github.com/lonelycode/botMaker) library, which provides primitives for building chatbots in Go, and more importantly, make it easier to create embeddings (context data) that your slackbot can tap into when coming up with responses.

## Quickstart

### 1. Pull the source code

```
git clone https://github.com/lonelycode/lurch.git
cd lurch
go build
```

This will create a `lurch` binary, we'll need that later.

### 2. Create a `.env` file:

```bash
echo export OPEN_API_KEY="YOUR OPENAI API KEY" >> .env
echo export PINECONE_KEY="YOUR PINECONE API KEY" >> .env
echo export PINECONE_URL="YOUR PINECONE INSTANCE URL" >> .env
echo export SLACK_APP_TOKEN="YOUR SLACK APP TOKEN" >> .env
echo export SLACK_BOT_TOKEN="YOUR SLACK BOT TOKEN" >> .env
```
You will need a few things in order to get the above values:
1. `OPEN_API_KEY` - An OpenAI API key, you can sign up [here](https://platform.openai.com/)
2. `PINECONE_KEY` and `PINECONE_URL` - head on over to [Pinicone.io](https://www.pinecone.io/) and set up an account, then launch a new index. 
3. `SLACK_APP_TOKEN` and `SLACK_BOT_TOKEN` are a faff to get, you will need to sign up for a new app in your app workspace and roughly follow [these instructions](https://www.twilio.com/blog/how-to-build-a-slackbot-in-socket-mode-with-python), only the parts about creating the bot in Slack workspace. 

### 3. Set up your bot configuration

Bot configurations are stored in directories, and have a simple format:
```
bot-name/
-- lurch.json
-- prompt.tpl
```

You can use the example bot config under the `bots/tyk` folder in the repo as a quickstart:

```bash 
cp -R bots/tyk bots/mybot
```

The configuration file will look something like this:

```json 
{
  "Bot": {
    "ID": "YOUR-PINECONE-NAMESPACE",
    "Model": "gpt-3.5-turbo",
    "Temp": 0.3,
    "TopP": 0,
    "FrequencyPenalty": 0,
    "PresencePenalty": 0,
    "MaxTokens": 4096,
    "TokenLimit": 4096
  },
  "Instructions": "You are an AI chatbot that is happy and helpful."
}
```

The key settings that you will need to configure are:
1. The `Bot.ID` field: this specifies the namespace the bot will use for pulling context embeddings for queries to ChatGPT, it also uses this namespace when learning via chat
2. The `Instructions` field: This specifies how your bot should behave, if you are working with nich content, it's worth including it's scope here as it will ground all the answers.

The remaining settings in the `Bot` section are all described elsewhere and are specific to OpenAI, you can find out more [here](https://towardsdatascience.com/gpt-3-parameters-and-prompt-design-1a595dc5b405).

### 4. Run the bot

The bot doesn't have a daemon mode or anything, but it should work as a systemd service, to quickstart, just run it from inside the source directory:
```bash
martin@Martins-Air lurch % ./lurch bots/tyk   
      
Unexpected event type received: connecting
Unexpected event type received: connected
Unexpected event type received: hello
```

Now you just need to add the bot to a slack channel and `@mention` it with a question or a command.

## Commands

There are two built in commands for lurch:

### `reset`

Lurch will keep each users conversation history in local memory, it will feed this back into GPT as part of it's prompt in order to ensure there is a context for the conversation. However, since GPT3.5 has a 4k token limit, at some point the chat history plus the extra context will cause the prompt to fail.

by typing `@Lurch reset`, the bot will wipe the local map of the conversation to start again. The bot will tell you how long the conversation history is (in messages, not tokens), in each response, as well as how many context objects were used for the prompt.

### `learn this`

You can correct lurch if it gives you a wrong answer, you can do this by simply responding to it with the correct answer like so:

```
@Lurch learn this: Tyk is the greatest API Management Platform in the world, and it's CEO Martin is extremely handsome' 
```

The bot will then save *entire chat history between you and the bot* as an embedding into pinecone, so it can reference it later. It will also wipe the current local conversation history so as not to accidentally store the same data twice.

It isn't recommended to train the bot this way! This is just a handy way to quickly correct mistakes during conversation.

## Bulk Learning

In order to teach a bot bulk information, you can use the botMaker 'learning' example.

Simply point it at a directory full of text, markdown or PDF files and it will start to create embeddings for you:

```bash 
git clone git@github.com:lonelycode/botMaker.git
cd botMaker/examples/learning
go build
./learning NAMESPACE /PATH/TO/LOAD
```

botMaker uses the same environment variables that Lurch uses, no additional settings should be required.
