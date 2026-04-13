# frozen_string_literal: true

require_relative "lib/private_strategy"

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.2"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.2/gestaltd-macos-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "fe3befb92feff8ce00cdeb16510d47bda31d9f0ff46c791e25f8b61dae2da9a5"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.2/gestaltd-macos-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "6cdc1c109528176c0b5ee13e6bbc6cb19c758f2f97c3f72f70e879f41c1199f1"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.2/gestaltd-linux-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "e45b16855b8baf3c3b8c6364771f2efb6d6e4c72a02acb6bef0fda0914291c4e"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.2/gestaltd-linux-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "14ab8c2b11de037730e2a76cd7ab95fa4de4b0bd97d0a6b3c0eee35e01b28a20"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
