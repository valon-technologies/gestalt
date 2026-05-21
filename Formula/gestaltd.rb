# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.4"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.4/gestaltd-macos-arm64.tar.gz"
      sha256 "c254a5ce515a062d5f315d922531ce141daa7c7548f8070aff793beb86c9efaf"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.4/gestaltd-macos-x86_64.tar.gz"
      sha256 "97a38deac81e038c24a9aba1112acca269139ea383203985ab740cbcd2ff8807"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.4/gestaltd-linux-arm64.tar.gz"
      sha256 "cffd1b768e8ed2d2a4763c31e6d9029f93df4a184010f6c8aa927699d85625f2"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.4/gestaltd-linux-x86_64.tar.gz"
      sha256 "a62348d420b98696d7c122478ac6b7914c1d98cb1427684a53c78d51ee02b610"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
