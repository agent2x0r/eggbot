from eggbot import bind, say, run


@bind("pub", flags="-", mask="!py")
def ping_pub(nick, host, handle, chan, text):
    say(chan, f"{nick}: pong from python")


if __name__ == "__main__":
    run()
