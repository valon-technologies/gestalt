# frozen_string_literal: true

require_relative "lib/private_strategy"

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.50"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.50/gestaltd-macos-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "f1d80eb0c725350ab170e087ade4a28c4b945dcb2815d8c5a4476c6daa737bdd"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.50/gestaltd-macos-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "45c724e96f840de2486c87db9babcb40bfb92f874b14c5f48136e5d5d4a982f7"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.50/gestaltd-linux-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "6a6e6a029c45c3decf8c8564ee0ebf85bd83a776d4e86eadc42f6fefc0fefd4e"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.50/gestaltd-linux-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "5e3b7a8a91ebd1418dbf7447b1d3dad0dbc53ff2918e739c7464f63e349768f4"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
