# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.12"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.12/gestalt-macos-arm64.tar.gz"
      sha256 "4c87579dfd4d4a44cc266b418d0cdec176253e24a39ebf446ef90b7674c5b700"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.12/gestalt-macos-x86_64.tar.gz"
      sha256 "ca6764aa73031aea840d0571f120ed581996c52cf106380904c7984a0a658a00"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.12/gestalt-linux-arm64.tar.gz"
      sha256 "d45569dc4b4e880f65482c6776b2035fa9f1426f8f6aa657079ba208de9b0518"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.12/gestalt-linux-x86_64.tar.gz"
      sha256 "b673f17a3eccb07987b3cb66e43eb962fee77b7044964aa94a57c663ef062228"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
