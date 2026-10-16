#!/usr/bin/env python3
"""Loopback-only static hosting simulator; never uploads or deploys anything."""
import argparse,http.server,pathlib,urllib.parse
parser=argparse.ArgumentParser()
parser.add_argument('--port',type=int,default=8084)
parser.add_argument('--headers',action='store_true',help='simulate CF _headers for this build')
args=parser.parse_args();root=pathlib.Path(__file__).resolve().parents[1]/'dist'
class Handler(http.server.BaseHTTPRequestHandler):
 def do_HEAD(self):self.serve(False)
 def do_GET(self):self.serve(True)
 def serve(self,body):
  path=urllib.parse.unquote(urllib.parse.urlsplit(self.path).path)
  name=path[1:] if path.startswith('/') else None
  if name in ('learn', 'docs', 'design'):
   query=urllib.parse.urlsplit(self.path).query
   self.send_response(301);self.send_header('Location',path+'/' + ('?'+query if query else ''));self.send_header('Content-Length','0');self.end_headers();return
  if name in ('', 'learn/', 'docs/', 'design/'):name+='index.html'
  valid=name is not None and '\\' not in name and '..' not in pathlib.PurePosixPath(name).parts and (root/name).is_file() and (root/name).resolve().is_relative_to(root.resolve())
  code=200 if valid else 404
  file=root/name if valid else root/'404.html'
  data=file.read_bytes()
  mime={'.html':'text/html; charset=utf-8','.js':'text/javascript','.css':'text/css','.wasm':'application/wasm','.json':'application/json','.ttf':'font/ttf','.zip':'application/zip'}.get(file.suffix,'text/plain')
  self.send_response(code);self.send_header('Content-Type',mime);self.send_header('Content-Length',str(len(data)))
  if args.headers:
   for line in (root/'_headers').read_text().splitlines():
    if line.startswith('  '):key,value=line.strip().split(':',1);self.send_header(key,value.strip())
  self.end_headers()
  if body:self.wfile.write(data)
 def log_message(self,*args):pass
http.server.ThreadingHTTPServer(('127.0.0.1',args.port),Handler).serve_forever()
