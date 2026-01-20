# frozen_string_literal: true

require "download_strategy"

# Custom download strategy for private GitHub releases.
# Requires HOMEBREW_GITHUB_API_TOKEN environment variable.
class GitHubPrivateRepositoryReleaseDownloadStrategy < CurlDownloadStrategy
  def initialize(url, name, version, **meta)
    super
    parse_url_pattern
    set_github_token
  end

  def parse_url_pattern
    url_pattern = %r{https://github\.com/([^/]+)/([^/]+)/releases/download/([^/]+)/(.+)}
    unless @url =~ url_pattern
      raise CurlDownloadStrategyError, "URL pattern not supported for private GitHub releases"
    end

    @owner, @repo, @tag, @filename = Regexp.last_match.captures
  end

  def set_github_token
    @github_token = ENV["HOMEBREW_GITHUB_API_TOKEN"] || gh_cli_token
    unless @github_token
      raise CurlDownloadStrategyError,
            "GitHub authentication required. Either:\n" \
            "  1. Run 'gh auth login' (recommended), or\n" \
            "  2. Set HOMEBREW_GITHUB_API_TOKEN with a token from https://github.com/settings/tokens"
    end
  end

  def gh_cli_token
    gh_path = ["/opt/homebrew/bin/gh", "/usr/local/bin/gh"].find { |p| File.exist?(p) }
    return nil unless gh_path

    token = `#{gh_path} auth token 2>/dev/null`.chomp
    token.empty? ? nil : token
  end

  def fetch(timeout: nil, **_options)
    ohai "Fetching #{@filename} from private repository"
    asset_id = resolve_asset_id
    download_url = "https://api.github.com/repos/#{@owner}/#{@repo}/releases/assets/#{asset_id}"

    curl_download(
      download_url,
      "--header", "Authorization: token #{@github_token}",
      "--header", "Accept: application/octet-stream",
      to: temporary_path,
      timeout: timeout
    )

    FileUtils.mv(temporary_path, cached_location)
    cached_location
  end

  private

  def resolve_asset_id
    release_url = "https://api.github.com/repos/#{@owner}/#{@repo}/releases/tags/#{@tag}"

    output, _, status = curl_output(
      "--header", "Authorization: token #{@github_token}",
      "--header", "Accept: application/vnd.github+json",
      release_url
    )

    unless status.success?
      raise CurlDownloadStrategyError, "Failed to fetch release info for #{@tag}"
    end

    release_data = JSON.parse(output)

    # Check for API error responses (e.g., 401 Bad credentials, 404 Not Found)
    if release_data["message"]
      case release_data["message"]
      when "Bad credentials"
        raise CurlDownloadStrategyError,
              "GitHub authentication failed. Your token may be invalid or expired.\n" \
              "  Run 'gh auth login' to re-authenticate, or\n" \
              "  Update HOMEBREW_GITHUB_API_TOKEN with a valid token"
      when "Not Found"
        raise CurlDownloadStrategyError,
              "Release '#{@tag}' not found. Either:\n" \
              "  - The release doesn't exist, or\n" \
              "  - Your token doesn't have access to this repository"
      else
        raise CurlDownloadStrategyError, "GitHub API error: #{release_data["message"]}"
      end
    end

    asset = release_data["assets"]&.find { |a| a["name"] == @filename }

    unless asset
      raise CurlDownloadStrategyError, "Asset '#{@filename}' not found in release #{@tag}"
    end

    asset["id"]
  end
end
