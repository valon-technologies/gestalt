# frozen_string_literal: true

require_relative "lib/private_strategy"

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.11"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.11/gestalt-macos-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "11c4c34e056ec7b853ff6d238803714723b3c5bffb84230f602bf7ece48c7bb4"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.11/gestalt-macos-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "72ca4d0c8b6ea6050c358a3cdf0a760a61d72a77a91ab4fbbe79a8c41c23c62a"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.11/gestalt-linux-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "8f5e103c2279bdf54deb06998fa4ffcb04d78730fcc82612553e60bcfcb959bf"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.11/gestalt-linux-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "adc37c199cfe96de6ddfafb87ca23140cc2c9319799ca2e20f9c82ac2480efb7"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
