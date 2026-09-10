# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.20"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.20/gestalt-macos-arm64.tar.gz"
      sha256 "a9d76db9e3a2f107fcb111783274fbbae8c920b861b9c6c78b42411d349706e0"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.20/gestalt-macos-x86_64.tar.gz"
      sha256 "7a9b88a18b85d1d4aab70a2a4aa90cbb08cf533b96147b31a33a6baf8c81b0ca"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.20/gestalt-linux-arm64.tar.gz"
      sha256 "a28c87990c8b8bae43cec49f44f62943d76efbfb44e511e57634b3b1a6694fe9"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.20/gestalt-linux-x86_64.tar.gz"
      sha256 "505aae6bdb84bd6bab2e39d5c8f174c7bfbc54a93ab3dc584f08f62c395c1f46"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
