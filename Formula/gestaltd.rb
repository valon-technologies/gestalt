# frozen_string_literal: true

require_relative "lib/private_strategy"

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.19"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.19/gestaltd-macos-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "8c93ce87af1e750c82259bf3dece24a437ccc249501fb6a4e813227c6ac02931"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.19/gestaltd-macos-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "866215e2d1abb37333b22e6758c2658335b8a0b2c02cf1c10d51186cf1e3082a"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.19/gestaltd-linux-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "7701931bcfa807c6c5d26df46bcf56cfa7afb2301896056fed04531bed741bce"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.19/gestaltd-linux-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "42f1e8d2c17b8f3e4460bd6aab926afc62c9d14feccbaee7279a1c681da4df54"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
