// Preloaded by the production server (node -r ./peer-ip.cjs server.js).
// Next.js does not expose the TCP peer address to route handlers, and it
// keeps a client-supplied X-Forwarded-For as is, so this records the real
// peer in a header the client cannot set. The API proxy forwards it.
const http = require('http');

const HEADER = 'x-pulse-peer-ip';
const emit = http.Server.prototype.emit;
http.Server.prototype.emit = function (event, req, ...rest) {
  if (event === 'request' && req && req.headers) {
    req.headers[HEADER] = (req.socket && req.socket.remoteAddress) || '';
  }
  return emit.call(this, event, req, ...rest);
};
