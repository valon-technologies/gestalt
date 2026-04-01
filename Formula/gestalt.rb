# frozen_string_literal: true

require_relative "lib/private_strategy"

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.27"
  license "Proprietary"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/v0.0.1-alpha.27/gestalt-macos-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "c004479f2e17650b829e9dcfd963727cc01cdfb1aa99b42dbcd2b267ad579bec"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/v0.0.1-alpha.27/gestalt-macos-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "e6e105ee6d14815f095dc27c855b711d799c53fb2562eb80b65db4c7c48f89b3"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/v0.0.1-alpha.27/gestalt-linux-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "06b67672fc3d8ab0c0a2e4745de8f5730985d57c1e13af7605030e58eaee8b98"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/v0.0.1-alpha.27/gestalt-linux-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "9d55dd1807f9278be88e713c44679de4a030aad2c87844743280f2668c763e06"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
