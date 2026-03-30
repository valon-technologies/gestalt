# frozen_string_literal: true

require_relative "lib/private_strategy"

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.12"
  license "Proprietary"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/v0.0.1-alpha.12/gestaltd-macos-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "e584b9a57245201e76b98fde234d6ef47a7c58377abad4bf696e519d18980c00"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/v0.0.1-alpha.12/gestaltd-macos-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "621040002cc04e7e29c0a8384b7f89830515c691486864efdb5c23e831b0cd50"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/v0.0.1-alpha.12/gestaltd-linux-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "c387fdf561e80bb00c522b570f1388851538b25b9b1d9f37d929dcbac173424b"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/v0.0.1-alpha.12/gestaltd-linux-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "3bd89a1a91bd8562d3fbd903506a085b26e0c41dd0db32ce7292f5beb18527a9"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
