import os, pty, select, time, struct, fcntl, termios, sys, pyte
from PIL import Image, ImageDraw, ImageFont
COLS, ROWS = 120, 38
OUT = sys.argv[1]
pid, fd = pty.fork()
if pid == 0:
    os.environ.update(TERM='xterm-256color', COLORTERM='truecolor', CAPRI_TRACE='0',
        RAG_DB=OUT+'/demo.db', WORKDIR=OUT+'/demo/workspace', AUTO_APPROVE='1')
    os.chdir('/home/madkoding/proyectos/capri')
    os.execv('./capri', ['./capri'])
fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack('HHHH', ROWS, COLS, 0, 0))
screen = pyte.Screen(COLS, ROWS); stream = pyte.ByteStream(screen)
def pump(t):
    end = time.time()+t
    while time.time() < end:
        r,_,_ = select.select([fd],[],[],0.05)
        if r:
            try: stream.feed(os.read(fd,65536))
            except OSError: return
def send(s, w=0.3): os.write(fd, s.encode()); pump(w)
F = ImageFont.truetype('/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf', 15)
FB = ImageFont.truetype('/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf', 15)
CW = int(F.getlength('M')); CH = 20
NAMES = {'black':(0,0,0),'red':(205,49,49),'green':(13,188,121),'brown':(229,229,16),'blue':(36,114,200),'magenta':(188,63,188),'cyan':(17,168,205),'white':(229,229,229),
 'brightblack':(102,102,102),'brightred':(241,76,76),'brightgreen':(35,209,139),'brightbrown':(245,245,67),'brightblue':(59,142,234),'brightmagenta':(214,112,214),'brightcyan':(41,184,219),'brightwhite':(255,255,255)}
BG0=(10,10,16); FG0=(210,214,224)
def col(c, d):
    if c=='default': return d
    if c in NAMES: return NAMES[c]
    if len(c)==6:
        try: return tuple(int(c[i:i+2],16) for i in (0,2,4))
        except: pass
    return d
def render(name):
    img = Image.new('RGB',(COLS*CW+24, ROWS*CH+24+28), BG0); d = ImageDraw.Draw(img)
    # window chrome
    d.rectangle([0,0,img.width,28], fill=(30,30,42))
    for i,c in enumerate([(255,95,86),(255,189,46),(39,201,63)]): d.ellipse([12+i*20,9,24+i*20,21],fill=c)
    d.text((img.width//2-30,6),'capri',font=F,fill=(150,154,170))
    for y in range(ROWS):
        for x in range(COLS):
            ch = screen.buffer[y][x]
            fg = col(ch.fg,FG0); bg = col(ch.bg,BG0)
            if ch.reverse: fg,bg = bg,fg
            px,py = 12+x*CW, 36+y*CH
            if bg!=BG0: d.rectangle([px,py,px+CW,py+CH],fill=bg)
            if ch.data.strip(): d.text((px,py),ch.data,font=FB if ch.bold else F,fill=fg)
    img.save(f'{OUT}/out/{name}.png'); print('saved',name)
def settle():
    quiet=0; end=time.time()+1.0
    while quiet<0.045 and time.time()<end:
        r,_,_=select.select([fd],[],[],0.045)
        if r:
            try: stream.feed(os.read(fd,65536))
            except OSError: return
            quiet=0
        else: quiet=0.05
def snap(n): settle(); render(n)
pump(0.8); snap('01-arranque')
pump(3.0); snap('02-reposo')
send('lista los archivos y luego dime cuántos hay',0.4); pump(0.5); snap('03-escribiendo'); send('\r',1.5); snap('04-pensando'); pump(10); snap('05-resultado')
send('borra los archivos .tmp',0.4); send('\r',2); snap('06-autorizacion'); pump(10); snap('07-borrado')
send('me llamo Camila y vivo en Valdivia',0.3); send('\r',6)
send('dónde vivo?',0.3); send('\r',2); pump(8); snap('08-memoria')
send('/help',0.3); send('\r',1.5); snap('09-ayuda')
send('/mem',0.3); send('\r',1.5); snap('10-mem'); send('\x1b',0.5)
send('\x03',0.8)
