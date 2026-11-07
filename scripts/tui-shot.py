# Captura la pantalla REAL de la TUI en un emulador de terminal (pyte) en varios momentos.
#   python3 -m venv /tmp/v && /tmp/v/bin/pip install pyte
#   TAIL=16 /tmp/v/bin/python scripts/tui-shot.py 130 46 [basic]   # columnas filas [basic = solo arranque y reposo]
import os, pty, select, time, struct, fcntl, termios, sys, pyte
COLS = int(sys.argv[1]); ROWS = int(sys.argv[2]) if len(sys.argv) > 2 else 48
script = sys.argv[3] if len(sys.argv) > 3 else 'full'
pid, fd = pty.fork()
if pid == 0:
    os.environ.update(TERM='xterm-256color', COLORTERM='truecolor', CAPRI_TRACE='0')
    binario = os.environ.get('CAPRI_BIN', 'capri')
    os.execvp(binario, [binario])
fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack('HHHH', ROWS, COLS, 0, 0))
screen = pyte.Screen(COLS, ROWS); stream = pyte.ByteStream(screen)
def pump(t):
    end = time.time() + t
    while time.time() < end:
        r, _, _ = select.select([fd], [], [], 0.05)
        if r:
            try: stream.feed(os.read(fd, 65536))
            except OSError: return
def send(s, wait=0.3): os.write(fd, s.encode()); pump(wait)
def settle():
    # espera un hueco entre fotogramas (la TUI escribe cada fotograma de una vez)
    quiet = 0
    end = time.time() + 1.0
    while quiet < 0.045 and time.time() < end:
        r, _, _ = select.select([fd], [], [], 0.045)
        if r:
            try: stream.feed(os.read(fd, 65536))
            except OSError: return
            quiet = 0
        else:
            quiet = 0.05
def snap(title):
    settle()
    lines = [l.rstrip() for l in screen.display]
    while lines and not lines[-1]: lines.pop()
    n = int(os.environ.get('TAIL', '0'))
    if n: lines = lines[-n:]
    print(f'\n━━━━ {title} ({COLS}x{ROWS}) ' + '━' * 20); print('\n'.join(lines))
pump(1.5);  snap('ARRANQUE')
pump(2.4);  snap('REPOSO')
if script == 'full':
    send('lista los archivos y luego dime cuántos hay', 0.4); send('\r', 7.0); snap('TRAS UNA ORDEN')
    for i in range(3):
        send('cuéntame algo sobre el mar', 0.3); send('\r', 5.0)
    send('\x1b[5~', 0.8); snap('RePág (scroll hacia arriba)')
    send('\x1b[6~', 0.5)
send('\x03', 0.8)
