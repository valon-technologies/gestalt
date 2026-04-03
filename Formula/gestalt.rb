# frozen_string_literal: true

require_relative "lib/private_strategy"

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.1"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.1/gestalt-macos-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "0d5c2a101221d104250015670e673570459c3bbf818ffd198ff0c9dfcfe909e3"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.1/gestalt-macos-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "2073dc477239ed656782874709570a0780743fde05e3926ea087ba5e3eaaf0b9"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.1/gestalt-linux-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "2e66bca9fe64f2a5b1ed86d2fa87c5b0b34504d491f790b89bad01516ab0ce88"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.1/gestalt-linux-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "484e126453104396c17e757d7a14d4ef518b3fb869570a5701d1f626afbba7f0"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
