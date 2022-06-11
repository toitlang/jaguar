import log show level_name
import log.target
import system.api.logging show LoggingService
import system.services show ServiceDefinition
import encoding.base64
import websocket
import monitor
import encoding.ubjson

service ::= LoggingServiceDefinition

install_logging_service:
  print "Installing logging service"
  service.install

add_log_listener session/websocket.Session:
  service.add_listener session

class LoggingServiceDefinition extends ServiceDefinition implements LoggingService:
  logs_/List := []
  console/bool := true
  logs/monitor.Channel
  sessions := []
  system_logging_service ::= target.StandardLoggingService_

  constructor:
    logs = monitor.Channel 20
    super "system/logging/jaguar" --major=1 --minor=2
    provides LoggingService.UUID LoggingService.MAJOR LoggingService.MINOR

    task --background:: process_logs

  handle pid/int client/int index/int arguments/any -> any:
    if index == LoggingService.LOG_INDEX:
      logs.send arguments
      return null

    unreachable

  log level/int message/string names/List? keys/List? values/List? trace/ByteArray? -> none:
    if console:
      system_logging_service.log level message names keys values null
      if trace:
        print_ "---"
        print_ "jag decode $(base64.encode trace)"
        print_ "---"

  process_logs:
    while true:
      msg := logs.receive
      if console:
        log msg[0] msg[1] msg[2] msg[3] msg[4] msg[5]

      //print "session.size: $sessions.size"
      sessions.do: | sess/websocket.Session | sess.send (ubjson.encode msg)
      //print "notified"

  add_listener session/websocket.Session:
    sessions.add session
    task --background::
      while true:
        catch --no-trace:
          msg := session.receive
          if msg == "quit" or not msg:
            print_ "Logging session ended"
            sessions.remove session
            break
          else if msg == "toggle_console":
            console = not console
          else:
            print_ "Unknown command: $msg"

