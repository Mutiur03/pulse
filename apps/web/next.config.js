const path = require('path');

/** @type {import('next').NextConfig} */
const nextConfig = {
  output: 'standalone',
  // Trace files from the monorepo root so workspace packages are bundled.
  outputFileTracingRoot: path.join(__dirname, '../..'),
  reactStrictMode: true,
  transpilePackages: ['@pulse/api-client'],
};

module.exports = nextConfig;
