/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",
  async rewrites() {
    const backend = process.env.TOOLSHED_API_URL;
    if (!backend) return [];
    return {
      beforeFiles: [
        {
          source: "/api/v1/:path*",
          destination: `${backend}/api/v1/:path*`,
        },
      ],
    };
  },
};

export default nextConfig;
