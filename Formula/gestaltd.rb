# frozen_string_literal: true

require_relative "lib/private_strategy"

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.7"
  license "Proprietary"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/v0.0.1-alpha.7/gestaltd-macos-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "4af2a70f4496470de0388df8efe1a3306e82acb3592bfc86836073160b4f180b"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/v0.0.1-alpha.7/gestaltd-macos-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "17a64c6a40fd01f3346ea5c1c9902ee505e1c4a7797f3a88a6c50ddfbfb74dbd"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/v0.0.1-alpha.7/gestaltd-linux-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "608e19ca6ad7351c7ea3d66bc4b183c28fd57acbe1437236f14392c6a3772b0d"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/v0.0.1-alpha.7/gestaltd-linux-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "fc1349d6cbd8e6fa52fdae535b77a4350643d8fe9914bd35fa656c8ce4428a27"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
