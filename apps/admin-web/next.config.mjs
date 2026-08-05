/** @type {import('next').NextConfig} */
const controlAPI = process.env.CONTROL_API_INTERNAL_URL ?? 'http://localhost:8080';

const nextConfig = {
  output: 'standalone',
  poweredByHeader: false,
  reactStrictMode: true,
  async rewrites() {
    return [{ source: '/api/:path*', destination: `${controlAPI}/api/:path*` }];
  }
};

export default nextConfig;
