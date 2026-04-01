# frozen_string_literal: true

require_relative "lib/private_strategy"

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.23"
  license "Proprietary"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/v0.0.1-alpha.23/gestaltd-macos-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "020c37faa7412fcb4722ad5f3375eca6a291d1457792774b6fb9c4df723cb765"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/v0.0.1-alpha.23/gestaltd-macos-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "4ee66af1085fb3c3d737585c088dafd2a252ff908e85f11ac80c435b0f1f9dba"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/v0.0.1-alpha.23/gestaltd-linux-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "dcff78d3bdb4cd8330e21a7745561bdb4a4e0d3cf14dc55f5f627d7691926282"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/v0.0.1-alpha.23/gestaltd-linux-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "5744c7d7f34002cbaac59794b04cbe63bdf7b3c3b2acd148915c4836bcd32fc9"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
