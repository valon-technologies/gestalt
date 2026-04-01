# frozen_string_literal: true

require_relative "lib/private_strategy"

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.29"
  license "Proprietary"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.29/gestalt-macos-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "044d11faa20e182307969ea8857a8d645387321cae4e835c2af444eaf8cde5d2"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.29/gestalt-macos-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "4678e37ef16ce0a956b90c20669499a3b1a8b96a01d84ad67554d8ceb7944edc"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.29/gestalt-linux-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "a292322a4ac7ef1bcc4e95fc00b4908737d65c5b65da3cfc109135db5cc9e5b3"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.29/gestalt-linux-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "f63a305d42b6d1c3f437a5ff9ba71e356135db4c185232d695c291b89a144bdd"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
