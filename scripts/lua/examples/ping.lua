-- Example eggdrop-style public bind.
bind("pub", "-", "!ping", "ping_pub")

function ping_pub(nick, host, handle, chan, text)
  say(chan, nick .. ": pong")
end
